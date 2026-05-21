package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Bridge feishu env override covers all the new env-var keys.
func TestBridgeFeishuEnvOverrides(t *testing.T) {
	env := map[string]string{
		"AGENT_CENTER_BRIDGE_FEISHU_ENABLED":           "true",
		"AGENT_CENTER_BRIDGE_FEISHU_APP_ID":            "envapp",
		"AGENT_CENTER_BRIDGE_FEISHU_APP_SECRET":        "envsecret",
		"AGENT_CENTER_BRIDGE_FEISHU_APP_SECRET_FILE":   "/secret",
		"AGENT_CENTER_BRIDGE_FEISHU_BASE_URL":          "https://custom",
	}
	cfg, err := Load(LoadOptions{
		Env: func(k string) (string, bool) {
			v, ok := env[k]
			return v, ok
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Bridge.Feishu.Enabled {
		t.Fatal("enabled lost")
	}
	if cfg.Bridge.Feishu.AppID != "envapp" {
		t.Fatalf("app_id: %s", cfg.Bridge.Feishu.AppID)
	}
	if cfg.Bridge.Feishu.AppSecret != "envsecret" {
		t.Fatalf("secret: %s", cfg.Bridge.Feishu.AppSecret)
	}
	if cfg.Bridge.Feishu.BaseURL != "https://custom" {
		t.Fatalf("base: %s", cfg.Bridge.Feishu.BaseURL)
	}
}

func TestBridgeFeishuEnabledInvalidBool(t *testing.T) {
	_, err := Load(LoadOptions{
		Env: func(k string) (string, bool) {
			if k == "AGENT_CENTER_BRIDGE_FEISHU_ENABLED" {
				return "maybe", true
			}
			return "", false
		},
	})
	if err == nil {
		t.Fatal("want err on bad bool")
	}
	if !strings.Contains(err.Error(), "boolean") {
		t.Fatalf("err: %v", err)
	}
}

func TestBridgeFeishuValidateRequiresAppIDWhenEnabled(t *testing.T) {
	_, err := Load(LoadOptions{
		Env: func(k string) (string, bool) {
			switch k {
			case "AGENT_CENTER_BRIDGE_FEISHU_ENABLED":
				return "true", true
			case "AGENT_CENTER_BRIDGE_FEISHU_APP_SECRET":
				return "sec", true
			}
			return "", false
		},
	})
	if err == nil || !strings.Contains(err.Error(), "app_id") {
		t.Fatalf("err: %v", err)
	}
}

func TestBridgeFeishuValidateRequiresSecretWhenEnabled(t *testing.T) {
	_, err := Load(LoadOptions{
		Env: func(k string) (string, bool) {
			switch k {
			case "AGENT_CENTER_BRIDGE_FEISHU_ENABLED":
				return "true", true
			case "AGENT_CENTER_BRIDGE_FEISHU_APP_ID":
				return "x", true
			}
			return "", false
		},
	})
	if err == nil || !strings.Contains(err.Error(), "app_secret") {
		t.Fatalf("err: %v", err)
	}
}

func TestBridgeFeishuSecretFromFile(t *testing.T) {
	dir := t.TempDir()
	secretPath := filepath.Join(dir, "secret")
	_ = os.WriteFile(secretPath, []byte("file-secret\n"), 0o600)
	cfg, err := Load(LoadOptions{
		Env: func(k string) (string, bool) {
			switch k {
			case "AGENT_CENTER_BRIDGE_FEISHU_ENABLED":
				return "true", true
			case "AGENT_CENTER_BRIDGE_FEISHU_APP_ID":
				return "app", true
			case "AGENT_CENTER_BRIDGE_FEISHU_APP_SECRET_FILE":
				return secretPath, true
			}
			return "", false
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Bridge.Feishu.AppSecret != "file-secret" {
		t.Fatalf("secret: %s", cfg.Bridge.Feishu.AppSecret)
	}
}

func TestBridgeFeishuSecretFileUnreadable(t *testing.T) {
	_, err := Load(LoadOptions{
		Env: func(k string) (string, bool) {
			switch k {
			case "AGENT_CENTER_BRIDGE_FEISHU_ENABLED":
				return "true", true
			case "AGENT_CENTER_BRIDGE_FEISHU_APP_ID":
				return "app", true
			case "AGENT_CENTER_BRIDGE_FEISHU_APP_SECRET_FILE":
				return "/proc/no/such/file", true
			}
			return "", false
		},
	})
	if err == nil || !strings.Contains(err.Error(), "app_secret_file") {
		t.Fatalf("err: %v", err)
	}
}

func TestBridgeFeishuYAMLLoad(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "cfg.yaml")
	secretPath := filepath.Join(dir, "secret")
	_ = os.WriteFile(secretPath, []byte("y"), 0o600)
	body := "server:\n  listen_addr: ':7000'\n  sqlite_path: '/tmp/x'\nidentity:\n  default_user: hayang\nbridge:\n  feishu:\n    enabled: true\n    app_id: yaml_app\n    app_secret_file: " + secretPath + "\n"
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(LoadOptions{Path: cfgPath})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Bridge.Feishu.AppID != "yaml_app" {
		t.Fatalf("app_id: %s", cfg.Bridge.Feishu.AppID)
	}
	if cfg.Bridge.Feishu.AppSecret != "y" {
		t.Fatalf("secret: %s", cfg.Bridge.Feishu.AppSecret)
	}
}

func TestBridgeFeishuYAMLRejectsUnknownBridgeKey(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "cfg.yaml")
	body := "server:\n  listen_addr: ':7000'\n  sqlite_path: '/tmp/x'\nidentity:\n  default_user: hayang\nbridge:\n  feishu:\n    enabled: false\n    app_id: x\n    unknown_field: 42\n"
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(LoadOptions{Path: cfgPath})
	if err == nil || !strings.Contains(err.Error(), "unknown_field") {
		t.Fatalf("err: %v", err)
	}
}

func TestBridgeFeishuEnvBoolFalseValues(t *testing.T) {
	for _, v := range []string{"0", "false", "no", "off", ""} {
		val := v
		_, err := Load(LoadOptions{
			Env: func(k string) (string, bool) {
				if k == "AGENT_CENTER_BRIDGE_FEISHU_ENABLED" {
					return val, true
				}
				return "", false
			},
		})
		if err != nil {
			t.Fatalf("bool %q: err=%v", v, err)
		}
	}
}
