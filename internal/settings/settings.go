// Package settings holds the admin-editable runtime configuration. Values are
// stored as JSON in SQLite and override the environment defaults at startup.
package settings

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

const (
	MinUploadBytes = 1 << 20  // 1 MiB
	MaxUploadBytes = 10 << 30 // 10 GiB
	MaxFileCount   = 100
	MaxTextBytes   = 1 << 20 // 1 MiB
	MaxExpiry      = 365 * 24 * 60 * 60
	MaxBackgrounds = 30
)

var uploadedBackgroundPattern = regexp.MustCompile(`^/media/backgrounds/[0-9a-f]{32}\.(?:jpg|png|webp|gif)$`)

// Background is one rotating stage image. Kind is one of:
//   - bing: Bing daily photo (URL may be empty)
//   - picsum: Picsum photo (URL may be empty)
//   - upload: file uploaded through the admin page, stored under /media/backgrounds/
//   - url: remote image added by URL
type Background struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	URL    string `json:"url,omitempty"`
	Label  string `json:"label"`
	Credit string `json:"credit,omitempty"`
}

// Settings is the full runtime configuration exposed to the admin page.
type Settings struct {
	MaxUploadBytes       int64        `json:"maxUploadBytes"`
	MaxFiles             int          `json:"maxFiles"`
	MaxTextBytes         int          `json:"maxTextBytes"`
	DefaultExpirySeconds int64        `json:"defaultExpirySeconds"`
	MaxExpirySeconds     int64        `json:"maxExpirySeconds"`
	DefaultMaxDownloads  int          `json:"defaultMaxDownloads"`
	Backgrounds          []Background `json:"backgrounds"`
}

// Defaults matches the values the app shipped with before the admin existed.
func Defaults() Settings {
	return Settings{
		MaxUploadBytes:       2 << 30,
		MaxFiles:             20,
		MaxTextBytes:         64 << 10,
		DefaultExpirySeconds: 7 * 24 * 60 * 60,
		MaxExpirySeconds:     365 * 24 * 60 * 60,
		DefaultMaxDownloads:  5,
		Backgrounds:          []Background{},
	}
}

// Validate rejects configurations that would break uploads or the public UI.
func (s *Settings) Validate() error {
	switch {
	case s.MaxUploadBytes < MinUploadBytes || s.MaxUploadBytes > MaxUploadBytes:
		return fmt.Errorf("上传上限需在 1 MB 到 10 GB 之间")
	case s.MaxFiles < 1 || s.MaxFiles > MaxFileCount:
		return fmt.Errorf("单次文件数需在 1 到 %d 之间", MaxFileCount)
	case s.MaxTextBytes < 1024 || s.MaxTextBytes > MaxTextBytes:
		return fmt.Errorf("文本上限需在 1 KB 到 1 MB 之间")
	case s.MaxExpirySeconds < 60 || s.MaxExpirySeconds > MaxExpiry:
		return fmt.Errorf("最长有效期需在 1 分钟到 365 天之间")
	case s.DefaultExpirySeconds < 60 || s.DefaultExpirySeconds > s.MaxExpirySeconds:
		return fmt.Errorf("默认有效期不能超过最长有效期")
	case s.DefaultMaxDownloads < 0 || s.DefaultMaxDownloads > 1000:
		return fmt.Errorf("默认取件设备数需在 0 到 1000 之间")
	case len(s.Backgrounds) > MaxBackgrounds:
		return fmt.Errorf("背景图最多 %d 张", MaxBackgrounds)
	}

	seen := make(map[string]bool, len(s.Backgrounds))
	for i := range s.Backgrounds {
		bg := &s.Backgrounds[i]
		bg.Label = strings.TrimSpace(bg.Label)
		bg.Credit = strings.TrimSpace(bg.Credit)
		if bg.ID == "" {
			return errors.New("背景图缺少标识")
		}
		if seen[bg.ID] {
			return errors.New("背景图标识重复")
		}
		seen[bg.ID] = true
		if bg.Label == "" {
			bg.Label = "自定义背景"
		}
		if len([]rune(bg.Label)) > 60 {
			bg.Label = string([]rune(bg.Label)[:60])
		}
		switch bg.Kind {
		case "bing", "picsum":
			// URL is optional for built-in sources.
		case "upload":
			if !uploadedBackgroundPattern.MatchString(bg.URL) {
				return errors.New("上传背景的地址无效")
			}
		case "url":
			parsed, err := url.Parse(bg.URL)
			if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
				return errors.New("背景图链接需为 http(s) 地址")
			}
		default:
			return errors.New("背景图类型无效")
		}
	}
	return nil
}

// Normalize clamps values loaded from storage so a damaged row cannot break
// the public endpoints. It does not validate user input.
func (s *Settings) Normalize() {
	if s.MaxUploadBytes < MinUploadBytes {
		s.MaxUploadBytes = MinUploadBytes
	}
	if s.MaxUploadBytes > MaxUploadBytes {
		s.MaxUploadBytes = MaxUploadBytes
	}
	if s.MaxFiles < 1 {
		s.MaxFiles = 1
	}
	if s.MaxFiles > MaxFileCount {
		s.MaxFiles = MaxFileCount
	}
	if s.MaxTextBytes < 1024 {
		s.MaxTextBytes = 1024
	}
	if s.MaxTextBytes > MaxTextBytes {
		s.MaxTextBytes = MaxTextBytes
	}
	if s.MaxExpirySeconds < 60 {
		s.MaxExpirySeconds = 60
	}
	if s.MaxExpirySeconds > MaxExpiry {
		s.MaxExpirySeconds = MaxExpiry
	}
	if s.DefaultExpirySeconds < 60 {
		s.DefaultExpirySeconds = 60
	}
	if s.DefaultExpirySeconds > s.MaxExpirySeconds {
		s.DefaultExpirySeconds = s.MaxExpirySeconds
	}
	if s.DefaultMaxDownloads < 0 {
		s.DefaultMaxDownloads = 0
	}
	if s.DefaultMaxDownloads > 1000 {
		s.DefaultMaxDownloads = 1000
	}
	if s.Backgrounds == nil {
		s.Backgrounds = []Background{}
	}
	if len(s.Backgrounds) > MaxBackgrounds {
		s.Backgrounds = s.Backgrounds[:MaxBackgrounds]
	}
}
