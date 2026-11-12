package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestLoadPhonesFromXLSX(t *testing.T) {
	set, err := loadPhonesFromXLSX("/tmp/test_allowlist.xlsx")
	if err != nil {
		t.Fatalf("load error: %v", err)
	}
	want := map[string]bool{
		"13800138000": true, // 内联字符串
		"13900139000": true, // 数字单元格
		"13700137000": true, // 共享字符串
	}
	for p := range want {
		if !set[p] {
			t.Errorf("missing phone %s; got %v", p, set)
		}
	}
	if len(set) != len(want) {
		t.Errorf("expected %d phones, got %d: %v", len(want), len(set), set)
	}
}

func TestAllowPhone(t *testing.T) {
	a := &phoneAllowlist{}
	const xlsx = "/tmp/test_allowlist.xlsx"

	// 1) 未配置：放行
	os.Unsetenv("WRAPPER_ALLOWLIST_EXCEL")
	if ok, enf, err := a.allowPhone("13500000000"); !ok || enf || err != nil {
		t.Errorf("case1 unset: ok=%v enf=%v err=%v", ok, enf, err)
	}

	// 2) 文件不存在：放行
	t.Setenv("WRAPPER_ALLOWLIST_EXCEL", "/no/such/file.xlsx")
	if ok, enf, err := a.allowPhone("13500000000"); !ok || enf || err != nil {
		t.Errorf("case2 missing file: ok=%v enf=%v err=%v", ok, enf, err)
	}

	// 3) 文件存在，号码在名单：放行
	t.Setenv("WRAPPER_ALLOWLIST_EXCEL", xlsx)
	if ok, enf, err := a.allowPhone("13800138000"); !ok || !enf || err != nil {
		t.Errorf("case3 in-list: ok=%v enf=%v err=%v", ok, enf, err)
	}

	// 4) 文件存在，号码不在名单：拒绝
	if ok, enf, err := a.allowPhone("13500000000"); ok || !enf || err != nil {
		t.Errorf("case4 not-in-list: ok=%v enf=%v err=%v", ok, enf, err)
	}
}

func TestParseSheetColumnEnv(t *testing.T) {
	const xlsx = "/tmp/test_allowlist.xlsx"

	// 显式指定「手机号」列：应识别该列
	t.Setenv("WRAPPER_ALLOWLIST_COLUMN", "手机号")
	set, err := loadPhonesFromXLSX(xlsx)
	if err != nil {
		t.Fatal(err)
	}
	if len(set) != 3 {
		t.Errorf("expected 3 for 手机号 column, got %d: %v", len(set), set)
	}

	// 指定不存在的列名：不收录任何号码（忽略其他列）
	t.Setenv("WRAPPER_ALLOWLIST_COLUMN", "电话")
	set2, err := loadPhonesFromXLSX(xlsx)
	if err != nil {
		t.Fatal(err)
	}
	if len(set2) != 0 {
		t.Errorf("expected 0 for unknown column, got %d: %v", len(set2), set2)
	}
}

func TestTestSMSHandler(t *testing.T) {
	// 1) 未设访问码 + 公网来源 → 403（不真正发短信）
	wrapperPass = ""
	req := httptest.NewRequest(http.MethodPost, "/test-sms", strings.NewReader(`{"phone":"13800138000"}`))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "8.8.8.8:1234"
	rec := httptest.NewRecorder()
	testSMSHandler(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("case1 public no-passcode: want 403 got %d", rec.Code)
	}

	// 2) 未设访问码 + 本机来源 + 格式错误 → 400
	req2 := httptest.NewRequest(http.MethodPost, "/test-sms", strings.NewReader(`{"phone":"123"}`))
	req2.Header.Set("Content-Type", "application/json")
	req2.RemoteAddr = "127.0.0.1:1234"
	rec2 := httptest.NewRecorder()
	testSMSHandler(rec2, req2)
	if rec2.Code != http.StatusBadRequest {
		t.Errorf("case2 loopback bad phone: want 400 got %d", rec2.Code)
	}

	// 3) 设访问码 + 错误访问码 → 403
	wrapperPass = "secret"
	defer func() { wrapperPass = "" }()
	req3 := httptest.NewRequest(http.MethodPost, "/test-sms", strings.NewReader(`{"phone":"13800138000","passcode":"wrong"}`))
	req3.Header.Set("Content-Type", "application/json")
	req3.RemoteAddr = "8.8.8.8:1234"
	rec3 := httptest.NewRecorder()
	testSMSHandler(rec3, req3)
	if rec3.Code != http.StatusForbidden {
		t.Errorf("case3 wrong passcode: want 403 got %d", rec3.Code)
	}
}

func TestSMSMock(t *testing.T) {
	t.Setenv("WRAPPER_SMS_MOCK", "1")
	t.Setenv("WRAPPER_SMS_MOCK_CODE", "123456")
	ok, msg := goldnetGainValidCode("13800138000")
	if !ok || !strings.Contains(msg, "123456") {
		t.Fatalf("mock send failed: ok=%v msg=%q", ok, msg)
	}
	if ok2, _ := goldnetVerifyCode("13800138000", "123456"); !ok2 {
		t.Error("mock verify correct code should pass")
	}
	// 验证码一次性使用，且错误码应失败
	if ok3, _ := goldnetVerifyCode("13800138000", "000000"); ok3 {
		t.Error("mock verify wrong code should fail")
	}
}
