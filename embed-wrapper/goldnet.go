package main

// GoldNet（金网络/中企云链）企业短信网关加解密客户端。
//
// 协议与 dify/api/libs/goldnet_crypto.py 完全对齐：
//   1. 随机生成 16 字节 AES 密钥（base64 字符串），用其 AES/CBC/PKCS7 加密明文（IV 前缀）
//   2. 用网关 RSA 公钥（PKCS1v15）加密该 base64 密钥字符串
//   3. 用 appSecret 做 HMAC-SHA256 签名（v2.0 签名串格式）
//   4. 请求体为 base64(IV+ciphertext)，签名与加密密钥放在请求头
//   5. 若响应头带 signature/aesKey/timestamp，则验签并用 RSA 私钥解出 AES 密钥再解密响应体
//
// 仅依赖 Go 标准库。

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

var phonePattern = regexp.MustCompile(`^1[3-9]\d{9}$`)

func validPhoneCN(phone string) bool { return phonePattern.MatchString(phone) }

// ---- RSA / AES 基础 ----

func normalizeKeyDER(raw, defaultType string) ([]byte, error) {
	raw = strings.TrimSpace(raw)
	if !strings.Contains(raw, "-----BEGIN") {
		raw = "-----BEGIN " + defaultType + "-----\n" + raw + "\n-----END " + defaultType + "-----"
	}
	block, _ := pem.Decode([]byte(raw))
	if block == nil {
		return nil, fmt.Errorf("invalid PEM key")
	}
	return block.Bytes, nil
}

func goldnetPublicKey() (*rsa.PublicKey, error) {
	der, err := normalizeKeyDER(os.Getenv("SMS_GOLDNET_PUBLIC_KEY"), "PUBLIC KEY")
	if err != nil {
		return nil, err
	}
	pub, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return nil, err
	}
	pk, ok := pub.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("not RSA public key")
	}
	return pk, nil
}

func goldnetPrivateKey() (*rsa.PrivateKey, error) {
	der, err := normalizeKeyDER(os.Getenv("SMS_GOLDNET_PRIVATE_KEY"), "RSA PRIVATE KEY")
	if err != nil {
		return nil, err
	}
	if k, err := x509.ParsePKCS1PrivateKey(der); err == nil {
		return k, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return nil, err
	}
	pk, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("not RSA private key")
	}
	return pk, nil
}

func pkcs7Pad(data []byte, blockSize int) []byte {
	pad := blockSize - len(data)%blockSize
	out := make([]byte, len(data)+pad)
	copy(out, data)
	for i := len(data); i < len(out); i++ {
		out[i] = byte(pad)
	}
	return out
}

func pkcs7Unpad(data []byte, blockSize int) ([]byte, error) {
	if len(data) == 0 || len(data)%blockSize != 0 {
		return nil, fmt.Errorf("bad padding")
	}
	pad := int(data[len(data)-1])
	if pad < 1 || pad > blockSize {
		return nil, fmt.Errorf("bad padding")
	}
	return data[:len(data)-pad], nil
}

func aesCBCEncrypt(plain, key []byte) (iv, ct []byte, err error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, err
	}
	iv = make([]byte, aes.BlockSize)
	if _, err = rand.Read(iv); err != nil {
		return nil, nil, err
	}
	padded := pkcs7Pad(plain, aes.BlockSize)
	mode := cipher.NewCBCEncrypter(block, iv)
	ct = make([]byte, len(padded))
	mode.CryptBlocks(ct, padded)
	return iv, ct, nil
}

func aesCBCDecrypt(ct, key, iv []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	if len(ct)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("bad ciphertext length")
	}
	mode := cipher.NewCBCDecrypter(block, iv)
	plain := make([]byte, len(ct))
	mode.CryptBlocks(plain, ct)
	return pkcs7Unpad(plain, aes.BlockSize)
}

func rsaEncrypt(pub *rsa.PublicKey, msg []byte) ([]byte, error) {
	return rsa.EncryptPKCS1v15(rand.Reader, pub, msg)
}

func rsaDecrypt(priv *rsa.PrivateKey, ct []byte) ([]byte, error) {
	return rsa.DecryptPKCS1v15(rand.Reader, priv, ct)
}

func hmacSHA256Hex(key, msg string) string {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(msg))
	return hex.EncodeToString(mac.Sum(nil))
}

func relativeURL(u string) string {
	parsed, err := url.Parse(u)
	if err != nil {
		return u
	}
	if parsed.RawQuery != "" {
		return parsed.Path + "?" + parsed.RawQuery
	}
	return parsed.Path
}

// buildV2SigningString 复刻 Java VersionTwoSignParameters.toString()：
// {appSecret}app_key{appKey}param{param}timestamp{timestamp}v2.0uri{relURL}{appSecret}
func buildV2SigningString(appKey, appSecret, param, timestamp, uri string) string {
	return fmt.Sprintf("%sapp_key%sparam%stimestamp%sv2.0uri%s%s",
		appSecret, appKey, param, timestamp, relativeURL(uri), appSecret)
}

func uuidHex() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func goldnetConfigured() bool {
	for _, k := range []string{
		"SMS_VALIDCODE_URL", "SMS_VERIFICATION_URL",
		"SMS_GOLDNET_APP_KEY", "SMS_GOLDNET_SECRET_KEY",
		"SMS_GOLDNET_PUBLIC_KEY", "SMS_GOLDNET_PRIVATE_KEY",
	} {
		if strings.TrimSpace(os.Getenv(k)) == "" {
			return false
		}
	}
	return true
}

// goldnetEncSign 加密明文并生成签名产物（请求用）。
func goldnetEncSign(plainText, appKey, appSecret, fullURL string) (encText, encAESKey, signature, timestamp string, err error) {
	pub, err := goldnetPublicKey()
	if err != nil {
		return "", "", "", "", err
	}
	aesKeyRaw := make([]byte, 16)
	if _, err = rand.Read(aesKeyRaw); err != nil {
		return
	}
	aesKeyB64 := base64.StdEncoding.EncodeToString(aesKeyRaw)
	iv, ct, err := aesCBCEncrypt([]byte(plainText), aesKeyRaw)
	if err != nil {
		return
	}
	encText = base64.StdEncoding.EncodeToString(append(iv, ct...))
	encAESKeyBytes, err := rsaEncrypt(pub, []byte(aesKeyB64))
	if err != nil {
		return
	}
	encAESKey = base64.StdEncoding.EncodeToString(encAESKeyBytes)
	timestamp = fmt.Sprintf("%d", time.Now().UnixMilli())
	signature = hmacSHA256Hex(appSecret, buildV2SigningString(appKey, appSecret, encText, timestamp, fullURL))
	return
}

// goldnetSend 发送一次加密签名请求，并验签/解密响应体。
func goldnetSend(fullURL, plainText, appKey, appSecret, pubPEM, privPEM string) (string, error) {
	encText, encAESKey, sig, ts, err := goldnetEncSign(plainText, appKey, appSecret, fullURL)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequest(http.MethodPost, fullURL, strings.NewReader(encText))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("alg", "HmacSHA256")
	req.Header.Set("signature", sig)
	req.Header.Set("aesKey", encAESKey)
	req.Header.Set("version", "2.0")
	req.Header.Set("appKey", appKey)
	req.Header.Set("timestamp", ts)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	body := string(bodyBytes)
	rspSig := resp.Header.Get("signature")
	rspTS := resp.Header.Get("timestamp")
	if rspTS == "" {
		rspTS = resp.Header.Get("timeStamp")
	}
	rspAESKey := resp.Header.Get("aesKey")
	if rspSig != "" && rspTS != "" && rspAESKey != "" {
		rel, _ := url.Parse(fullURL)
		return goldnetVerifyDecrypt(body, rspAESKey, rspSig, rspTS, appKey, appSecret, privPEM, rel.Path)
	}
	return body, nil
}

func goldnetVerifyDecrypt(encText, encAESKey, sig, ts, appKey, appSecret, privPEM, path string) (string, error) {
	if !hmac.Equal(
		[]byte(sig),
		[]byte(hmacSHA256Hex(appSecret, buildV2SigningString(appKey, appSecret, encText, ts, path))),
	) {
		return "", fmt.Errorf("response signature mismatch")
	}
	priv, err := goldnetPrivateKey()
	if err != nil {
		return "", err
	}
	aesKeyEnc, derr := base64.StdEncoding.DecodeString(encAESKey)
	if derr != nil {
		return "", derr
	}
	aesKeyB64Bytes, err := rsaDecrypt(priv, aesKeyEnc)
	if err != nil {
		return "", err
	}
	aesKeyRaw, err := base64.StdEncoding.DecodeString(string(aesKeyB64Bytes))
	if err != nil {
		return "", err
	}
	encBytes, err := base64.StdEncoding.DecodeString(encText)
	if err != nil {
		return "", err
	}
	if len(encBytes) < aes.BlockSize {
		return "", fmt.Errorf("short ciphertext")
	}
	iv := encBytes[:aes.BlockSize]
	ct := encBytes[aes.BlockSize:]
	plain, err := aesCBCDecrypt(ct, aesKeyRaw, iv)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

// ---- 业务封装 ----

func codeIsOK(v any) bool {
	switch x := v.(type) {
	case float64:
		return fmt.Sprintf("%.0f", x) == "200"
	case string:
		return x == "200"
	case int:
		return x == 200
	case int64:
		return x == 200
	}
	return false
}

func firstMsg(j map[string]any, fallback string) string {
	if m, ok := j["msg"].(string); ok && m != "" {
		return m
	}
	return fallback
}

func parseGoldNetResponse(resp string, isSend bool) (bool, string) {
	var j map[string]any
	if err := json.Unmarshal([]byte(resp), &j); err != nil {
		return false, "验证码服务返回格式异常"
	}
	if isSend {
		if codeIsOK(j["code"]) {
			return true, ""
		}
		return false, firstMsg(j, "获取验证码失败")
	}
	if j["code"] == nil || !codeIsOK(j["code"]) {
		return false, firstMsg(j, "验证码验证失败")
	}
	if s, ok := j["success"].(bool); ok && !s {
		return false, firstMsg(j, "验证码验证失败")
	}
	return true, ""
}

// goldnetGainValidCode 发送短信验证码。
// ---- 模拟短信（WRAPPER_SMS_MOCK=1 时启用）----
// 不调用真实网关，由本服务自行生成并校验验证码，用于本地演示/测试。
func smsMockEnabled() bool {
	v := strings.TrimSpace(os.Getenv("WRAPPER_SMS_MOCK"))
	return strings.EqualFold(v, "1") || strings.EqualFold(v, "true")
}

type mockSMSCode struct {
	code string
	exp  int64
}

var (
	mockSMSMu    sync.Mutex
	mockSMSCodes = map[string]mockSMSCode{}
)

// genMockCode 生成 6 位数字验证码。
func genMockCode() string {
	b := make([]byte, 6)
	for i := range b {
		var n [1]byte
		_, _ = rand.Read(n[:])
		b[i] = '0' + n[0]%10
	}
	return string(b)
}

func goldnetGainValidCode(phone string) (bool, string) {
	if !validPhoneCN(phone) {
		return false, "手机号格式不正确"
	}
	if smsMockEnabled() {
		code := strings.TrimSpace(os.Getenv("WRAPPER_SMS_MOCK_CODE"))
		if code == "" {
			code = genMockCode()
		}
		mockSMSMu.Lock()
		mockSMSCodes[phone] = mockSMSCode{code: code, exp: time.Now().Add(5 * time.Minute).Unix()}
		mockSMSMu.Unlock()
		log.Printf("[sms-mock] 模拟发送验证码 phone=%s code=%s", phone, code)
		return true, "模拟模式：验证码 " + code + "（未真实发送，仅供演示/测试）"
	}
	if !goldnetConfigured() {
		return false, "短信网关未配置"
	}
	body := map[string]any{
		"accessKey": os.Getenv("SMS_ACCESS_KEY"),
		"sceneCode": os.Getenv("SMS_SCENE_CODE"),
		"receiver":  phone,
		"tenantId":  os.Getenv("SMS_TENANT_ID"),
		"businessId": uuidHex(),
		"data":       map[string]string{"signName": os.Getenv("SMS_SIGN_NAME")},
	}
	plain, _ := json.Marshal(body)
	resp, err := goldnetSend(
		os.Getenv("SMS_VALIDCODE_URL"),
		string(plain),
		os.Getenv("SMS_GOLDNET_APP_KEY"),
		os.Getenv("SMS_GOLDNET_SECRET_KEY"),
		os.Getenv("SMS_GOLDNET_PUBLIC_KEY"),
		os.Getenv("SMS_GOLDNET_PRIVATE_KEY"),
	)
	if err != nil {
		return false, "获取验证码失败"
	}
	return parseGoldNetResponse(resp, true)
}

// goldnetVerifyCode 校验短信验证码。
func goldnetVerifyCode(phone, code string) (bool, string) {
	if !validPhoneCN(phone) {
		return false, "手机号格式不正确"
	}
	if smsMockEnabled() {
		mockSMSMu.Lock()
		defer mockSMSMu.Unlock()
		mc, ok := mockSMSCodes[phone]
		if !ok {
			return false, "验证码错误或已过期"
		}
		if time.Now().Unix() > mc.exp {
			delete(mockSMSCodes, phone)
			return false, "验证码已过期"
		}
		if mc.code != code {
			return false, "验证码错误"
		}
		delete(mockSMSCodes, phone)
		return true, "验证成功"
	}
	if !goldnetConfigured() {
		return false, "短信网关未配置"
	}
	body := map[string]any{
		"accessKey": os.Getenv("SMS_ACCESS_KEY"),
		"receiver":  phone,
		"sceneCode": os.Getenv("SMS_SCENE_CODE"),
		"validCode": code,
		"needBck":   false,
	}
	plain, _ := json.Marshal(body)
	resp, err := goldnetSend(
		os.Getenv("SMS_VERIFICATION_URL"),
		string(plain),
		os.Getenv("SMS_GOLDNET_APP_KEY"),
		os.Getenv("SMS_GOLDNET_SECRET_KEY"),
		os.Getenv("SMS_GOLDNET_PUBLIC_KEY"),
		os.Getenv("SMS_GOLDNET_PRIVATE_KEY"),
	)
	if err != nil {
		return false, "验证码验证失败"
	}
	return parseGoldNetResponse(resp, false)
}
