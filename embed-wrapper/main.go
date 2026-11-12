package main

import (
	"archive/zip"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// 极薄包装页：员工登录 → 嵌入 WeKnora 智能体（支持多智能体切换），并按人统计使用情况。
//
// 设计要点：
//   - publish token 只存在于本服务端（从嵌入链接的 #token= 解析），浏览器拿不到。
//   - 员工在包装页用「手机号 + 短信验证码」登录（复用 GoldNet 企业短信网关），
//     登录成功后 owner 即已验证的手机号，用于按人统计使用情况。
//   - 服务端用 publish token 调 WeKnora /exchange，把 owner 签名进会话 token，
//     再经 postMessage 注入 WeKnora iframe。owner 来自服务端，不可被浏览器伪造。
//   - 支持多智能体：直接粘贴 WeKnora 分享链接即可，自动解析出 base/渠道/token。
//     /embed-token?channel=ID 必须命中配置白名单，防止给任意渠道换 token。
//
// 配置：
//   WEKNORA_EMBED_URLS      唯一配置项，逗号分隔的 WeKnora 分享链接，每项可带 "|名称"：
//                           "http://host/embed/A#token=em_x|售前助手,http://host/embed/B#token=em_y|客服助手"
//                           单智能体也用此项（只填一条即可）。
//   ---- 短信网关（GoldNet）----
//   SMS_VALIDCODE_URL       发码地址
//   SMS_VERIFICATION_URL    验码地址
//   SMS_ACCESS_KEY          网关 accessKey
//   SMS_TENANT_ID           租户 ID
//   SMS_SCENE_CODE          场景码（如 SMSLOGIN）
//   SMS_SIGN_NAME           短信签名
//   SMS_GOLDNET_APP_KEY     应用 Key
//   SMS_GOLDNET_SECRET_KEY  应用密钥（HMAC 签名用）
//   SMS_GOLDNET_PUBLIC_KEY  网关 RSA 公钥（加密 AES 密钥）
//   SMS_GOLDNET_PRIVATE_KEY 网关 RSA 私钥（解密响应用）
//   ---- 通用 ----
//   WRAPPER_PORT            本服务端口，默认 :8088
//   WRAPPER_PASSCODE        可选访问码；为空则不校验
//   WRAPPER_ORIGIN          本服务对外 origin（须加入每个渠道的 allowed_origins）
//   WRAPPER_COOKIE_MAXDAYS  登录态保留天数（N 天免登录），默认 7；<=0 则关浏览器即失效
//   WRAPPER_COOKIE_SECURE   true 时 cookie 加 Secure（仅 HTTPS 入口部署时启用），默认 false
//   WRAPPER_ALLOWLIST_EXCEL  可选手机号白名单 xlsx；仅允许名单内号码发码，文件不存在则放行
//   WRAPPER_ALLOWLIST_COLUMN 白名单表中手机号所在列的表头名；留空默认认「手机号」「手机号码」
//   WRAPPER_SMS_MOCK         1/true 时启用模拟短信：不调真实网关，自行生成/校验验证码（演示/测试）
//   WRAPPER_SMS_MOCK_CODE    模拟模式固定验证码；留空则每次随机 6 位

var (
	wrapperPort = getenv("WRAPPER_PORT", ":8088")
	wrapperPass = os.Getenv("WRAPPER_PASSCODE")
	agents      = parseAgents()
)

type agent struct {
	ID           string `json:"id"`
	Label        string `json:"label"`
	Base         string `json:"base"` // WeKnora 的 origin（非机密，下发前端用于拼 iframe）
	PublishToken string `json:"-"`   // 渠道 publish token，禁止下发前端
}

// parseAgents 从唯一的 WEKNORA_EMBED_URLS 读取一组分享链接，解析出 base/渠道/token。
func parseAgents() []agent {
	raw := strings.TrimSpace(os.Getenv("WEKNORA_EMBED_URLS"))
	if raw == "" {
		return nil
	}
	out := parseAgentURLs(raw)
	for i := range out {
		if out[i].Label == "" {
			if len(out) == 1 {
				out[i].Label = "智能体"
			} else {
				out[i].Label = fmt.Sprintf("智能体%d", i+1)
			}
		}
	}
	return out
}

// parseAgentURLs 解析一组分享链接。每条可用 "|名称" 附加显示名。
func parseAgentURLs(raw string) []agent {
	var out []agent
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		urlStr, label, _ := strings.Cut(part, "|")
		urlStr = strings.TrimSpace(urlStr)
		label = strings.TrimSpace(label)
		base, ch, tok, err := parseEmbedURL(urlStr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARN: 跳过无效嵌入链接 %q: %v\n", urlStr, err)
			continue
		}
		out = append(out, agent{ID: ch, Label: label, Base: base, PublishToken: tok})
	}
	return out
}

// parseEmbedURL 从分享链接解析出 base / channelID / publish token。
// 链接形如 http://host/embed/{channel_id}#token={publish_token}
func parseEmbedURL(raw string) (base, channelID, token string, err error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return
	}
	if u.Scheme == "" || u.Host == "" {
		err = fmt.Errorf("缺少 scheme/host")
		return
	}
	base = u.Scheme + "://" + u.Host
	segs := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(segs) < 2 || segs[0] != "embed" {
		err = fmt.Errorf("path 须为 /embed/{channel_id}")
		return
	}
	channelID = segs[1]
	for _, kv := range strings.Split(u.Fragment, "&") {
		k, v, _ := strings.Cut(kv, "=")
		if k == "token" {
			token = v
		}
	}
	if token == "" {
		err = fmt.Errorf("fragment 中缺少 token=")
		return
	}
	return
}

func agentByID(id string) (agent, bool) {
	for _, a := range agents {
		if a.ID == id {
			return a, true
		}
	}
	return agent{}, false
}

func getenv(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func main() {
	if len(agents) == 0 {
		fmt.Fprintln(os.Stderr, "ERROR: 必须设置环境变量 WEKNORA_EMBED_URLS（WeKnora 分享链接，逗号分隔，每项可带 |名称）")
		os.Exit(1)
	}
	for _, a := range agents {
		if a.ID == "" || a.Base == "" || a.PublishToken == "" {
			fmt.Fprintf(os.Stderr, "ERROR: 智能体 %q 配置不完整（缺 base/channel/token）\n", a.Label)
			os.Exit(1)
		}
	}
	http.HandleFunc("/", loginPage)
	http.HandleFunc("/send-code", sendCodeHandler)
	http.HandleFunc("/login", loginHandler)
	http.HandleFunc("/app", appPage)
	http.HandleFunc("/logout", logoutHandler)
	http.HandleFunc("/embed-token", embedTokenHandler)
	http.HandleFunc("/test-sms", testSMSHandler) // 运维手动发送测试短信，验证网关
	fmt.Printf("embed-wrapper 监听 %s  (agents=%d)\n", wrapperPort, len(agents))
	if err := http.ListenAndServe(wrapperPort, nil); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// ---- owner cookie（base64 避免中文/特殊字符破坏 Cookie）----

// cookieMaxAgeSeconds 返回 owner cookie 的 MaxAge（秒）。
// 由 WRAPPER_COOKIE_MAXDAYS 控制（默认 7 天）；<=0 表示会话级（浏览器关闭即失效）。
func cookieMaxAgeSeconds() int {
	v := strings.TrimSpace(os.Getenv("WRAPPER_COOKIE_MAXDAYS"))
	if v == "" {
		return 7 * 24 * 3600
	}
	d, err := strconv.Atoi(v)
	if err != nil || d <= 0 {
		return -1
	}
	return d * 24 * 3600
}

func setOwnerCookie(w http.ResponseWriter, owner string) {
	v := base64.URLEncoding.EncodeToString([]byte(owner))
	c := &http.Cookie{
		Name:     "owner",
		Value:    v,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   cookieMaxAgeSeconds(),
	}
	// 仅当浏览器到本服务入口为 HTTPS 时设为 true（如生产反代场景），否则 cookie 不会被发送。
	if strings.EqualFold(os.Getenv("WRAPPER_COOKIE_SECURE"), "true") {
		c.Secure = true
	}
	http.SetCookie(w, c)
}

func ownerFromReq(r *http.Request) string {
	c, err := r.Cookie("owner")
	if err != nil || c.Value == "" {
		return ""
	}
	b, err := base64.URLEncoding.DecodeString(c.Value)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// ---- 登录（手机号 + 短信验证码）----

type rateLimiter struct {
	mu       sync.Mutex
	lastSend map[string]int64 // phone -> 上次发码时间(ms)
	daySend  map[string]int   // phone:日期 -> 当日发码次数
	fail     map[string]int   // phone -> 近期验码失败次数
}

var smsLimiter = &rateLimiter{
	lastSend: map[string]int64{},
	daySend:  map[string]int{},
	fail:     map[string]int{},
}

func (l *rateLimiter) canSend(phone string) (bool, string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now().UnixMilli()
	if t, ok := l.lastSend[phone]; ok && now-t < 60_000 {
		return false, "发送过于频繁，请 60 秒后再试"
	}
	key := phone + ":" + time.Now().Format("2006-01-02")
	if l.daySend[key] >= 20 {
		return false, "今日发送次数已达上限"
	}
	l.lastSend[phone] = now
	l.daySend[key]++
	return true, ""
}

func (l *rateLimiter) noteFail(phone string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.fail[phone]++
}

func (l *rateLimiter) locked(phone string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.fail[phone] >= 5
}

func (l *rateLimiter) reset(phone string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.fail, phone)
}

// readLoginInput 同时支持 JSON 与表单输入。
func readLoginInput(r *http.Request) (phone, code, passcode string) {
	if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		var m map[string]string
		json.NewDecoder(r.Body).Decode(&m)
		return m["phone"], m["code"], m["passcode"]
	}
	return r.FormValue("phone"), r.FormValue("code"), r.FormValue("passcode")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func loginPage(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if ownerFromReq(r) != "" {
		http.Redirect(w, r, "/app", http.StatusSeeOther)
		return
	}
	html := strings.ReplaceAll(loginHTML, "{{REQUIRE_PASSCODE}}",
		map[bool]string{true: "true", false: "false"}[wrapperPass != ""])
	html = strings.ReplaceAll(html, "{{MOCK_BANNER}}", mockBannerHTML())
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, html)
}

// mockBannerHTML 在演示模式（模拟短信）下返回登录页顶部提示条，否则为空。
func mockBannerHTML() string {
	if smsMockEnabled() {
		return `<div class="mock-banner">⚠️ 演示模式（模拟短信）：验证码不会真实发送，点击「获取验证码」后请在本页下方提示中获取验证码</div>`
	}
	return ""
}

// ---- 手机号白名单（可选）----
// WRAPPER_ALLOWLIST_EXCEL 指向一个 xlsx 文件，从中读取手机号列；仅允许名单内号码发码。
//   - 未设置该变量：关闭白名单，所有格式正确的号码均可发码。
//   - 设置但文件不存在：同样放行（按需求：文件缺失不阻断登录）。
//   - 文件存在：扫描所有工作表，收集符合中国大陆手机号格式的值；号码不在名单内则拒绝。

type phoneAllowlist struct {
	mu      sync.Mutex
	modTime time.Time
	phones  map[string]bool
}

var allowlist = &phoneAllowlist{}

// allowPhone 返回 (allowed, enforced, err)。
// enforced=false 表示未配置或文件不存在，调用方应放行；
// enforced=true 表示已启用，allowed 指示号码是否在名单内。
func (a *phoneAllowlist) allowPhone(phone string) (bool, bool, error) {
	path := strings.TrimSpace(os.Getenv("WRAPPER_ALLOWLIST_EXCEL"))
	if path == "" {
		return true, false, nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return true, false, nil // 文件不存在：放行
		}
		return false, false, err // 权限等其他错误：保守拒绝
	}
	if a.phones == nil || a.modTime != info.ModTime() {
		set, e := loadPhonesFromXLSX(path)
		if e != nil {
			return false, true, e
		}
		a.phones = set
		a.modTime = info.ModTime()
		log.Printf("[allowlist] loaded %d phones from %s", len(a.phones), path)
	}
	return a.phones[phone], true, nil
}

// loadPhonesFromXLSX 纯标准库解析 xlsx，扫描所有工作表，收集符合手机号格式的值。
func loadPhonesFromXLSX(path string) (map[string]bool, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	set := map[string]bool{}
	var sst []string
	for _, f := range zr.File {
		if f.Name == "xl/sharedStrings.xml" {
			if data, e := readZipFile(f); e == nil {
				sst = parseSharedStrings(data)
			}
		}
	}
	for _, f := range zr.File {
		if !strings.HasPrefix(f.Name, "xl/worksheets/sheet") || !strings.HasSuffix(f.Name, ".xml") {
			continue
		}
		data, e := readZipFile(f)
		if e != nil {
			continue
		}
		parseSheet(data, sst, set)
	}
	return set, nil
}

func readZipFile(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

type xlsSST struct {
	SI []struct {
		T []string `xml:"t"`
	} `xml:"si"`
}

// stripXMLNamespaces 移除 XML 中的 xmlns 声明，使 encoding/xml 能按 local name 匹配
// （Go 的 xml 包在 tag 未指定命名空间时只匹配无命名空间元素，而 xlsx 元素均带默认命名空间）。
func stripXMLNamespaces(b []byte) []byte {
	return xmlNsRE.ReplaceAll(b, nil)
}

var xmlNsRE = regexp.MustCompile(`\s+xmlns(:\w+)?="[^"]*"`)

func parseSharedStrings(data []byte) []string {
	data = stripXMLNamespaces(data)
	var s xlsSST
	_ = xml.Unmarshal(data, &s)
	out := make([]string, 0, len(s.SI))
	for _, si := range s.SI {
		out = append(out, strings.Join(si.T, ""))
	}
	return out
}

type xlsCell struct {
	T  string `xml:"t,attr"`
	V  string `xml:"v"`
	IS struct {
		T string `xml:"t"`
	} `xml:"is"`
}

func cellText(c xlsCell, sst []string) string {
	switch c.T {
	case "s":
		if idx, e := strconv.Atoi(strings.TrimSpace(c.V)); e == nil && idx >= 0 && idx < len(sst) {
			return sst[idx]
		}
	case "inlineStr":
		return c.IS.T
	}
	return c.V
}

type xlsSheet struct {
	SheetData struct {
		Rows []struct {
			Cells []xlsCell `xml:"c"`
		} `xml:"row"`
	} `xml:"sheetData"`
}

// parseSheet 仅识别目标表头列（忽略其他列）。
// 目标列名优先取环境变量 WRAPPER_ALLOWLIST_COLUMN；未设置时默认兼容「手机号」「手机号码」。
// 先找到第一个含目标表头的行作为表头行，其后续数据行只取这些列。
func parseSheet(data []byte, sst []string, set map[string]bool) {
	data = stripXMLNamespaces(data)
	var s xlsSheet
	if xml.Unmarshal(data, &s) != nil {
		return
	}
	targetCols := map[string]bool{}
	if col := strings.TrimSpace(os.Getenv("WRAPPER_ALLOWLIST_COLUMN")); col != "" {
		targetCols[col] = true
	} else {
		targetCols["手机号"] = true
		targetCols["手机号码"] = true
	}
	phoneCols := map[int]bool{}
	headerFound := false
	for _, row := range s.SheetData.Rows {
		if !headerFound {
			for ci, c := range row.Cells {
				v := strings.TrimSpace(cellText(c, sst))
				if targetCols[v] {
					phoneCols[ci] = true
				}
			}
			if len(phoneCols) > 0 {
				headerFound = true
			}
			continue // 表头行（及之前的行）不收录
		}
		for ci, c := range row.Cells {
			if !phoneCols[ci] {
				continue
			}
			v := strings.TrimSpace(cellText(c, sst))
			if validPhoneCN(v) {
				set[v] = true
			}
		}
	}
}

// sendCodeHandler 发送短信验证码（带频控）。
func sendCodeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	phone, _, _ := readLoginInput(r)
	phone = strings.TrimSpace(phone)
	if !validPhoneCN(phone) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "msg": "手机号格式不正确"})
		return
	}
	// 手机号白名单校验：enforced=true 且不在名单内则拒绝。
	if allowed, enforced, err := allowlist.allowPhone(phone); err != nil {
		log.Printf("[allowlist] load error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "msg": "服务暂时不可用"})
		return
	} else if enforced && !allowed {
		writeJSON(w, http.StatusForbidden, map[string]any{"success": false, "msg": "手机号未授权，无法登录"})
		return
	}
	if smsLimiter.locked(phone) {
		writeJSON(w, http.StatusTooManyRequests, map[string]any{"success": false, "msg": "尝试次数过多，请稍后再试"})
		return
	}
	if ok, msg := smsLimiter.canSend(phone); !ok {
		writeJSON(w, http.StatusTooManyRequests, map[string]any{"success": false, "msg": msg})
		return
	}
	ok, msg := goldnetGainValidCode(phone)
	writeJSON(w, http.StatusOK, map[string]any{"success": ok, "msg": msg})
}

// testSMSHandler 提供手动发送一条真实验证码的能力，用于运维验证短信网关配置。
// 保护：未设访问码时仅允许本机/内网；设了访问码则须提供正确访问码。其余逻辑与
// sendCodeHandler 一致（格式校验、白名单、频控），避免被滥用。
func testSMSHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		if wrapperPass == "" && !isLoopback(r) {
			http.Error(w, "仅允许本机/内网访问", http.StatusForbidden)
			return
		}
		renderTestSMSPage(w)
		return
	}
	phone, _, passcode := readLoginInput(r)
	phone = strings.TrimSpace(phone)
	if wrapperPass != "" && passcode != wrapperPass {
		writeJSON(w, http.StatusForbidden, map[string]any{"success": false, "msg": "访问码错误"})
		return
	}
	if wrapperPass == "" && !isLoopback(r) {
		writeJSON(w, http.StatusForbidden, map[string]any{"success": false, "msg": "仅允许本机/内网访问"})
		return
	}
	if !validPhoneCN(phone) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "msg": "手机号格式不正确"})
		return
	}
	if allowed, enforced, err := allowlist.allowPhone(phone); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "msg": "白名单加载失败"})
		return
	} else if enforced && !allowed {
		writeJSON(w, http.StatusForbidden, map[string]any{"success": false, "msg": "手机号未授权（不在白名单）"})
		return
	}
	if smsLimiter.locked(phone) {
		writeJSON(w, http.StatusTooManyRequests, map[string]any{"success": false, "msg": "尝试次数过多，请稍后再试"})
		return
	}
	if ok, msg := smsLimiter.canSend(phone); !ok {
		writeJSON(w, http.StatusTooManyRequests, map[string]any{"success": false, "msg": msg})
		return
	}
	ok, msg := goldnetGainValidCode(phone)
	writeJSON(w, http.StatusOK, map[string]any{"success": ok, "msg": msg})
}

// isLoopback 判断请求是否来自本机或内网，用于限制测试短信接口暴露面。
func isLoopback(r *http.Request) bool {
	host := r.RemoteAddr
	if i := strings.LastIndex(host, ":"); i >= 0 {
		host = host[:i]
	}
	host = strings.Trim(host, "[]")
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate()
}

const testSMSPageHTML = `<!doctype html><html lang="zh"><head><meta charset="utf-8">
<title>测试短信发送</title>
<style>body{font-family:system-ui,-apple-system;max-width:420px;margin:48px auto;padding:0 16px;color:#222}
input,button{font-size:15px;padding:9px;margin:6px 0;width:100%}button{background:#1677ff;color:#fff;border:0;border-radius:6px;cursor:pointer}
#r{margin-top:14px;white-space:pre-wrap;font-size:14px}</style></head>
<body><h3>测试短信发送</h3>
<p style="color:#888;font-size:13px">向指定手机号发送一条真实验证码，用于验证短信网关配置。仅本机/内网或持有访问码可发送。</p>
<input id="phone" placeholder="手机号"><br>
{{PASSCODE_INPUT}}
<button onclick="send()">发送测试短信</button>
<div id="r"></div>
<script>
async function send(){
  var phone=document.getElementById('phone').value.trim();
  var pc=document.getElementById('passcode')?document.getElementById('passcode').value:'';
  var r=document.getElementById('r'); r.textContent='发送中…';
  try{
    var resp=await fetch('/test-sms',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({phone:phone,passcode:pc})});
    var j=await resp.json();
    r.textContent=(j.success?'✅ ':'❌ ')+(j.msg||('HTTP '+resp.status));
  }catch(e){r.textContent='请求失败: '+e;}
}
</script></body></html>`

func renderTestSMSPage(w http.ResponseWriter) {
	pc := ""
	if wrapperPass != "" {
		pc = `<input id="passcode" placeholder="访问码"><br>`
	}
	html := strings.Replace(testSMSPageHTML, "{{PASSCODE_INPUT}}", pc, 1)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(html))
}

// loginHandler 校验短信验证码，成功后 owner = 已验证手机号。
func loginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	phone, code, passcode := readLoginInput(r)
	phone = strings.TrimSpace(phone)
	code = strings.TrimSpace(code)
	if !validPhoneCN(phone) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "msg": "手机号格式不正确"})
		return
	}
	if wrapperPass != "" && passcode != wrapperPass {
		writeJSON(w, http.StatusForbidden, map[string]any{"success": false, "msg": "访问码错误"})
		return
	}
	if smsLimiter.locked(phone) {
		writeJSON(w, http.StatusTooManyRequests, map[string]any{"success": false, "msg": "尝试次数过多，请稍后再试"})
		return
	}
	ok, msg := goldnetVerifyCode(phone, code)
	if !ok {
		smsLimiter.noteFail(phone)
		writeJSON(w, http.StatusUnauthorized, map[string]any{"success": false, "msg": msg})
		return
	}
	smsLimiter.reset(phone)
	setOwnerCookie(w, phone)
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

// ---- 嵌入页：多智能体列表 + iframe + postMessage 注入 token ----

// htmlEscape 转义注入到页面模板的少量服务端值（如登录手机号），防止 XSS。
func htmlEscape(s string) string {
	return strings.NewReplacer("<", "&lt;", ">", "&gt;", "&", "&amp;", "\"", "&quot;").Replace(s)
}

func appPage(w http.ResponseWriter, r *http.Request) {
	owner := ownerFromReq(r)
	if owner == "" {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	agentsJSON, _ := json.Marshal(agents)
	safe := strings.ReplaceAll(string(agentsJSON), "<", "\\u003c")
	safe = strings.ReplaceAll(safe, ">", "\\u003e")
	html := strings.ReplaceAll(appHTML, "{{AGENTS_JSON}}", safe)
	html = strings.ReplaceAll(html, "{{OWNER}}", htmlEscape(owner))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, html)
}

// embedTokenHandler：服务端用对应渠道的 publish token 换带 owner 的会话 token。
// channel 必须命中配置白名单，否则拒绝。
func embedTokenHandler(w http.ResponseWriter, r *http.Request) {
	owner := ownerFromReq(r)
	if owner == "" {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	channelID := strings.TrimSpace(r.URL.Query().Get("channel"))
	if channelID == "" {
		http.Error(w, `{"error":"missing channel"}`, http.StatusBadRequest)
		return
	}
	a, ok := agentByID(channelID)
	if !ok {
		http.Error(w, `{"error":"unknown channel"}`, http.StatusForbidden)
		return
	}
	token, exp, err := mintToken(r, a, owner)
	if err != nil {
		mintFailed(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"token": token, "expiresIn": exp})
}

// logoutHandler 清除 owner cookie 并跳回登录页。
func logoutHandler(w http.ResponseWriter, r *http.Request) {
	c := &http.Cookie{
		Name:     "owner",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	}
	if strings.EqualFold(os.Getenv("WRAPPER_COOKIE_SECURE"), "true") {
		c.Secure = true
	}
	http.SetCookie(w, c)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// mintToken：调 WeKnora /exchange 换带 owner 的会话 token。
func mintToken(r *http.Request, a agent, owner string) (string, int, error) {
	body, _ := json.Marshal(map[string]string{"owner": owner})
	u := fmt.Sprintf("%s/api/v1/embed/%s/exchange", strings.TrimRight(a.Base, "/"), a.ID)
	log.Printf("[embed-token] minting channel=%s base=%q origin=%s", a.ID, a.Base, wrapperOrigin(r))
	req, _ := http.NewRequest(http.MethodPost, u, strings.NewReader(string(body)))
	req.Header.Set("Authorization", "Embed "+a.PublishToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", wrapperOrigin(r)) // 须与渠道 allowed_origins 一致

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("[embed-token] channel=%s request error: %v", a.ID, err)
		return "", 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		log.Printf("[embed-token] channel=%s unexpected HTTP %d", a.ID, resp.StatusCode)
		return "", 0, fmt.Errorf("status %d", resp.StatusCode)
	}
	var out struct {
		Data struct {
			SessionToken string `json:"session_token"`
			ExpiresIn    int    `json:"expires_in"`
		} `json:"data"`
	}
	if json.NewDecoder(resp.Body).Decode(&out) != nil || out.Data.SessionToken == "" {
		log.Printf("[embed-token] channel=%s empty/invalid token response", a.ID)
		return "", 0, fmt.Errorf("empty token")
	}
	return out.Data.SessionToken, out.Data.ExpiresIn, nil
}

// mintFailed 返回统一的 mint 错误；WRAPPER_DEBUG=1 时附带真实错误便于排查。
func mintFailed(w http.ResponseWriter, err error) {
	if strings.EqualFold(os.Getenv("WRAPPER_DEBUG"), "1") && err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "mint failed", "detail": err.Error()})
		return
	}
	http.Error(w, `{"error":"mint failed"}`, http.StatusBadGateway)
}

func wrapperOrigin(r *http.Request) string {
	if v := os.Getenv("WRAPPER_ORIGIN"); v != "" {
		return strings.TrimRight(v, "/")
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

const loginHTML = `<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>内部智能体</title>
<style>
*{box-sizing:border-box}
body{margin:0;min-height:100vh;display:flex;align-items:center;justify-content:center;padding:24px;
  font-family:-apple-system,BlinkMacSystemFont,"Segoe UI","PingFang SC","Hiragino Sans GB","Microsoft YaHei",sans-serif;
  background:linear-gradient(135deg,#eef2ff 0%,#f6f7fb 45%,#e0f2fe 100%)}
.card{width:360px;max-width:100%;background:#fff;border-radius:20px;padding:40px 36px 32px;
  box-shadow:0 20px 60px rgba(15,23,42,.10),0 2px 8px rgba(15,23,42,.05)}
.logo{width:52px;height:52px;border-radius:14px;margin:0 auto 18px;display:flex;align-items:center;justify-content:center;
  background:linear-gradient(135deg,#4f7cff,#6d5cff);box-shadow:0 8px 20px rgba(79,124,255,.35)}
.logo svg{width:26px;height:26px;fill:#fff}
h1{margin:0 0 4px;font-size:20px;font-weight:600;color:#1e293b;text-align:center}
.sub{margin:0 0 26px;font-size:13px;color:#94a3b8;text-align:center}
label{display:block;font-size:12px;color:#64748b;margin:14px 0 6px}
input{width:100%;height:44px;padding:0 14px;font-size:14px;color:#1e293b;background:#f8fafc;
  border:1px solid #e2e8f0;border-radius:10px;outline:none;transition:border-color .15s,box-shadow .15s,background .15s}
input::placeholder{color:#a8b3c5}
input:focus{border-color:#4f7cff;background:#fff;box-shadow:0 0 0 3px rgba(79,124,255,.12)}
.code-row{display:flex;gap:10px}
.code-row input{flex:1;min-width:0}
#send{flex:0 0 auto;height:44px;padding:0 14px;font-size:13px;font-weight:500;color:#4f7cff;background:#eef2ff;
  border:0;border-radius:10px;cursor:pointer;white-space:nowrap;transition:background .15s,color .15s}
#send:hover:not(:disabled){background:#e0e9ff}
#send:disabled{color:#94a3b8;background:#f1f5f9;cursor:default}
#login{width:100%;height:44px;margin-top:24px;border:0;border-radius:10px;font-size:15px;font-weight:600;color:#fff;
  background:linear-gradient(135deg,#4f7cff,#6d5cff);cursor:pointer;
  box-shadow:0 6px 16px rgba(79,124,255,.30);transition:box-shadow .15s,transform .1s,opacity .15s}
#login:hover:not(:disabled){box-shadow:0 8px 22px rgba(79,124,255,.40)}
#login:active:not(:disabled){transform:translateY(1px)}
#login:disabled{opacity:.6;cursor:default}
#msg{margin:14px 0 0;font-size:13px;line-height:1.5;text-align:center;min-height:20px}
#msg.err{color:#dc2626}
#msg.ok{color:#16a34a}
.mock-banner{margin:0 0 18px;padding:10px 14px;font-size:12.5px;line-height:1.5;color:#92400e;background:#fef3c7;border:1px solid #fde68a;border-radius:12px;text-align:center}
</style>
</head><body><div class="card">{{MOCK_BANNER}}
<div class="logo"><svg viewBox="0 0 24 24"><path d="M12 3C7.03 3 3 6.58 3 11c0 2.09.9 4 2.38 5.42L4.5 20.5l3.9-1.55A9.7 9.7 0 0 0 12 19c4.97 0 9-3.58 9-8s-4.03-8-9-8z"/></svg></div>
<h1>内部智能体</h1>
<label for="phone">手机号</label>
<input id="phone" type="tel" maxlength="11" placeholder="请输入 11 位手机号" autofocus />
<label for="code">短信验证码</label>
<div class="code-row">
  <input id="code" type="text" inputmode="numeric" maxlength="6" placeholder="6 位验证码" />
  <button id="send" type="button">获取验证码</button>
</div>
<div id="pass-wrap" style="display:none">
  <label for="passcode">访问码</label>
  <input id="passcode" type="password" placeholder="请输入访问码" />
</div>
<button id="login">登录并开始对话</button>
<div id="msg"></div>
</div>
<script>
const REQUIRE_PASSCODE = {{REQUIRE_PASSCODE}};
if (REQUIRE_PASSCODE) document.getElementById('pass-wrap').style.display = 'block';
function $(id){return document.getElementById(id);}
function setMsg(t, ok){var m=$('msg');m.textContent=t;m.className=ok?'ok':'err';}
var timer=null;
function startCountdown(){
  var btn=$('send'), n=60;
  btn.disabled=true; btn.textContent='重新发送 '+n+'s';
  clearInterval(timer);
  timer=setInterval(function(){
    n--;
    if(n<=0){ clearInterval(timer); btn.disabled=false; btn.textContent='获取验证码'; return; }
    btn.textContent='重新发送 '+n+'s';
  },1000);
}
$('send').onclick = async function(){
  var phone = $('phone').value.trim();
  if(!/^1[3-9]\d{9}$/.test(phone)){ setMsg('手机号格式不正确'); return; }
  setMsg('');
  this.disabled = true;
  try{
    var r = await fetch('/send-code',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({phone:phone})});
    var j = await r.json();
    if(j.success){ setMsg(j.msg || '验证码已发送', true); startCountdown(); }
    else { setMsg(j.msg || '发送失败'); this.disabled = false; }
  }catch(e){ setMsg('网络错误，请稍后再试'); this.disabled = false; }
};
async function doLogin(){
  var phone = $('phone').value.trim();
  var code = $('code').value.trim();
  var passcode = $('passcode').value;
  if(!/^1[3-9]\d{9}$/.test(phone)){ setMsg('手机号格式不正确'); return; }
  if(!code){ setMsg('请输入短信验证码'); return; }
  var btn=$('login');
  btn.disabled=true; btn.textContent='登录中…';
  try{
    var r = await fetch('/login',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({phone:phone,code:code,passcode:passcode})});
    var j = await r.json().catch(function(){return {success:false,msg:'登录失败'};});
    if(j.success){ location.href = '/app'; return; }
    setMsg(j.msg || '登录失败');
  }catch(e){ setMsg('网络错误，请稍后再试'); }
  btn.disabled=false; btn.textContent='登录并开始对话';
}
$('login').onclick = doLogin;
['phone','code','passcode'].forEach(function(id){
  $(id).addEventListener('keydown', function(e){ if(e.key==='Enter') doLogin(); });
});
</script></body></html>`

const appHTML = `<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8"><title>内部智能体</title>
<style>
html,body{margin:0;height:100%;font-family:system-ui,-apple-system,"Segoe UI","PingFang SC","Microsoft YaHei",sans-serif}
#app{display:flex;height:100%;background:#f1f5f9}
/* 侧栏 */
#side{width:232px;background:#0f172a;color:#e2e8f0;padding:20px 0;flex:0 0 auto;display:flex;flex-direction:column;overflow:auto}
.brand{display:flex;align-items:center;gap:10px;padding:0 18px 16px}
.brand .dot{width:30px;height:30px;border-radius:9px;background:linear-gradient(135deg,#4f7cff,#6d5cff);display:flex;align-items:center;justify-content:center;color:#fff;font-weight:600;font-size:14px}
.brand .name{font-size:15px;font-weight:600;color:#fff;line-height:1.2}
.brand .desc{font-size:11px;color:#94a3b8}
#side h4{margin:0 0 10px;padding:0 18px;font-size:12px;font-weight:500;letter-spacing:.04em;color:#64748b}
.agent-btn{display:flex;align-items:center;gap:10px;width:100%;text-align:left;background:none;border:0;color:#cbd5e1;
  padding:10px 18px;font-size:14px;cursor:pointer;position:relative;transition:background .15s,color .15s}
.agent-btn:hover{background:#1e293b;color:#fff}
.agent-btn.active{background:#1e293b;color:#fff}
.agent-btn .av{width:26px;height:26px;border-radius:50%;flex:0 0 auto;display:flex;align-items:center;justify-content:center;
  font-size:12px;font-weight:600;color:#fff;background:#334155}
.agent-btn.active .av{background:#2563eb}
.agent-btn .txt{flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.agent-btn::before{content:"";position:absolute;left:0;top:8px;bottom:8px;width:3px;border-radius:0 3px 3px 0;background:#2563eb;opacity:0;transition:opacity .15s}
.agent-btn.active::before{opacity:1}
/* 舞台 */
#stage{flex:1;min-width:0;background:#fff}
#stage iframe{width:100%;height:100%;border:0;display:block;opacity:0;transition:opacity .25s ease}
#stage iframe.ready{opacity:1}
/* 顶部条（单智能体时显示） */
#topbar{display:none;align-items:center;gap:10px;height:56px;padding:0 18px;background:#0f172a;color:#fff;flex:0 0 auto}
#topbar .dot{width:28px;height:28px;border-radius:8px;background:linear-gradient(135deg,#4f7cff,#6d5cff);display:flex;align-items:center;justify-content:center;font-weight:600;color:#fff}
#topbar .name{font-size:15px;font-weight:600}
/* 当前登录用户 */
.user{display:flex;align-items:center;gap:8px;margin-top:auto;padding:12px 18px;border-top:1px solid #1e293b}
.user .uav{width:24px;height:24px;border-radius:50%;background:#334155;color:#fff;display:flex;align-items:center;justify-content:center;font-size:12px;flex:0 0 auto}
.user .unum{font-size:13px;color:#cbd5e1}
#topbar .user{margin-left:auto}
#topbar .user .unum{color:#e2e8f0}
.logout{margin-left:auto;border:0;background:transparent;color:#94a3b8;font-size:12px;cursor:pointer;padding:4px 8px;border-radius:6px;transition:background .15s,color .15s}
.logout:hover{background:#1e293b;color:#fff}
#topbar .logout{margin-left:auto;color:#cbd5e1}
#topbar .logout:hover{background:rgba(255,255,255,.12);color:#fff}
/* 窄屏：侧栏转顶部横向 */
@media (max-width:640px){
  #app{flex-direction:column}
  #side{width:100%;flex-direction:row;padding:10px;gap:8px;overflow-x:auto}
  .brand,#side h4{display:none}
  .agent-btn{border-radius:999px;padding:8px 14px;width:auto}
  .agent-btn .av{display:none}
  .agent-btn::before{display:none}
  .agent-btn.active{background:#2563eb}
  .user{display:none}
}
</style></head>
<body><div id="app">
<header id="topbar"><span class="dot">智</span><span class="name" id="topname"></span><span class="user" id="topuser"></span><button type="button" class="logout" onclick="location.href='/logout'">退出</button></header>
<aside id="side">
  <div class="brand"><span class="dot">智</span><div><div class="name">内部智能体</div><div class="desc">企业知识助手</div></div></div>
  <h4>智能体</h4>
  <div id="list"></div>
  <div class="user"><span class="uav">用</span><span class="unum" id="userphone"></span><button type="button" class="logout" onclick="location.href='/logout'">退出</button></div>
</aside>
<main id="stage"></main>
</div>
<script>
window.addEventListener('error', function (e) {
  var d = document.getElementById('__err');
  if (!d) {
    d = document.createElement('div'); d.id = '__err';
    d.style.cssText = 'position:fixed;left:0;right:0;bottom:0;z-index:9999;background:#dc2626;color:#fff;font:12px/1.4 monospace;padding:8px 12px;white-space:pre-wrap';
    document.body.appendChild(d);
  }
  d.textContent = 'JS Error: ' + (e.message || e.error) + (e.filename ? ' @' + e.lineno : '');
});
const AGENTS = {{AGENTS_JSON}};
const OWNER = "{{OWNER}}";
let activeChannel = AGENTS.length ? AGENTS[0].id : null;

function maskPhone(p) {
  if (!p || p.length < 7) return p || '';
  return p.slice(0, 3) + '****' + p.slice(-4);
}
const phoneText = maskPhone(OWNER);
const phoneEl = document.getElementById('userphone');
if (phoneEl) phoneEl.textContent = phoneText;
const topUserEl = document.getElementById('topuser');
if (topUserEl) topUserEl.textContent = phoneText;

// 单智能体：隐藏侧栏，改用顶部条。
const single = AGENTS.length <= 1;
if (single) {
  document.getElementById('side').style.display = 'none';
  document.getElementById('app').style.flexDirection = 'column';
  const tb = document.getElementById('topbar');
  tb.style.display = 'flex';
  document.getElementById('topname').textContent = AGENTS.length ? AGENTS[0].label : '内部智能体';
}

function avatarText(label) {
  const s = (label || '').trim();
  return s ? s.charAt(0) : '智';
}

const list = document.getElementById('list');
AGENTS.forEach(function (a) {
  const b = document.createElement('button');
  b.type = 'button';
  b.className = 'agent-btn';
  b.dataset.id = a.id;
  b.innerHTML = '<span class="av">' + avatarText(a.label) + '</span><span class="txt">' + (a.label || '智能体') + '</span>';
  b.onclick = function () { loadAgent(a.id); };
  list.appendChild(b);
});

const frames = new Map();

function loadAgent(id) {
  if (!id) return;
  activeChannel = id;
  const a = AGENTS.find(function (x) { return x.id === id; });
  document.querySelectorAll('.agent-btn').forEach(function (b) {
    b.classList.toggle('active', b.dataset.id === id);
  });
  let frame = frames.get(id);
  if (!frame) {
    frame = document.createElement('iframe');
    frame.src = a.base + '/embed/' + encodeURIComponent(id);
    frame.allow = 'clipboard-write';
    frame.onload = function () { frame.classList.add('ready'); };
    frames.set(id, frame);
    document.getElementById('stage').appendChild(frame);
  }
  frames.forEach(function (f, cid) {
    f.style.display = (cid === id) ? 'block' : 'none';
  });
}

// 监听 WeKnora 嵌入页发起的 bootstrap_request，回传服务端签发的会话 token。
window.addEventListener('message', function (e) {
  const d = e.data || {};
  if (d.source === 'weknora-embed' && d.type === 'bootstrap_request') {
    fetch('/embed-token?channel=' + encodeURIComponent(activeChannel))
      .then(function (r) { return r.json(); })
      .then(function (j) {
        if (!j.token) return;
        e.source.postMessage(
          { source: 'weknora-host', type: 'provide_token', token: j.token, channel_id: activeChannel },
          '*');
      });
  }
});

if (activeChannel) loadAgent(activeChannel);
</script></body></html>`
