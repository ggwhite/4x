package prompt

import (
	"log/slog"
	"os"

	"github.com/ggwhite/4x/internal/protocol"
)

var localeNames = map[string]string{
	"en":    "English",
	"zh-TW": "繁體中文",
	"zh-CN": "简体中文",
	"ja":    "日本語",
	"ko":    "한국어",
	"es":    "Español",
	"fr":    "Français",
	"de":    "Deutsch",
	"pt":    "Português",
	"vi":    "Tiếng Việt",
	"th":    "ภาษาไทย",
}

// ResolveLocale 依 user config 與環境變數解析目前的 locale code 與顯示名稱。
func ResolveLocale() (code, name string) {
	ucfg, err := protocol.ReadUserConfig()
	if err != nil {
		slog.Warn("failed to read user config", "error", err)
	}
	if ucfg.Locale != "" {
		code = ucfg.Locale
	} else {
		code = localeFromEnv()
	}
	name = localeNames[code]
	if name == "" {
		name = code
	}
	return code, name
}

func localeFromEnv() string {
	lang := os.Getenv("LANG")
	if lang == "" {
		lang = os.Getenv("LC_ALL")
	}
	if lang == "" {
		return "en"
	}
	for i, c := range lang {
		if c == '.' || c == '@' {
			lang = lang[:i]
			break
		}
	}
	zhMapping := map[string]string{
		"zh_TW": "zh-Hant", "zh_HK": "zh-Hant",
		"zh_CN": "zh-Hans", "zh": "zh-Hans",
	}
	if mapped, ok := zhMapping[lang]; ok {
		return mapped
	}
	if lang == "C" || lang == "POSIX" {
		return "en"
	}
	for i, c := range lang {
		if c == '_' || c == '-' {
			return lang[:i]
		}
	}
	return lang
}
