package settings

import "testing"

func TestDefaultsValidate(t *testing.T) {
	current := Defaults()
	if err := current.Validate(); err != nil {
		t.Fatalf("defaults should validate: %v", err)
	}
}

func TestValidateRejectsOutOfRangeValues(t *testing.T) {
	cases := map[string]func(*Settings){
		"tiny upload":          func(s *Settings) { s.MaxUploadBytes = 1024 },
		"huge upload":          func(s *Settings) { s.MaxUploadBytes = MaxUploadBytes + 1 },
		"zero files":           func(s *Settings) { s.MaxFiles = 0 },
		"too many files":       func(s *Settings) { s.MaxFiles = MaxFileCount + 1 },
		"tiny text":            func(s *Settings) { s.MaxTextBytes = 10 },
		"default over max":     func(s *Settings) { s.DefaultExpirySeconds = s.MaxExpirySeconds + 1 },
		"negative downloads":   func(s *Settings) { s.DefaultMaxDownloads = -1 },
		"too many backgrounds": func(s *Settings) { s.Backgrounds = make([]Background, MaxBackgrounds+1) },
		"duplicate background": func(s *Settings) { s.Backgrounds = []Background{{ID: "a", Kind: "bing"}, {ID: "a", Kind: "bing"}} },
		"upload outside media": func(s *Settings) {
			s.Backgrounds = []Background{{ID: "a", Kind: "upload", URL: "https://evil.example/x.png"}}
		},
		"invalid remote scheme": func(s *Settings) { s.Backgrounds = []Background{{ID: "a", Kind: "url", URL: "javascript:alert(1)"}} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			current := Defaults()
			mutate(&current)
			if err := current.Validate(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestValidateNormalizesLabels(t *testing.T) {
	current := Defaults()
	current.Backgrounds = []Background{{ID: "a", Kind: "bing", Label: "  "}}
	if err := current.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if current.Backgrounds[0].Label != "自定义背景" {
		t.Fatalf("label = %q", current.Backgrounds[0].Label)
	}
}

func TestNormalizeClampsStoredValues(t *testing.T) {
	current := Settings{
		MaxUploadBytes:       0,
		MaxFiles:             0,
		MaxTextBytes:         0,
		MaxExpirySeconds:     0,
		DefaultExpirySeconds: 0,
		DefaultMaxDownloads:  -5,
	}
	current.Normalize()
	if current.MaxUploadBytes != MinUploadBytes || current.MaxFiles != 1 || current.MaxTextBytes != 1024 {
		t.Fatalf("normalized = %+v", current)
	}
	if current.DefaultExpirySeconds > current.MaxExpirySeconds {
		t.Fatalf("default expiry exceeds max: %+v", current)
	}
	if current.DefaultMaxDownloads != 0 || current.Backgrounds == nil {
		t.Fatalf("normalized = %+v", current)
	}
}
