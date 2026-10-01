package authn

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFinancialConsentControlsAndPreview(t *testing.T) {
	for _, lang := range []string{"de", "en"} {
		for _, writes := range []bool{false, true} {
			for _, financial := range []bool{false, true} {
				var out bytes.Buffer
				if err := consentTemplate.Execute(&out, map[string]any{"Client": "Test connector", "User": "Test user", "CSRF": "local-preview", "EnableWrites": writes, "RequestFinancial": financial, "Lang": lang}); err != nil {
					t.Fatal(err)
				}
				html := out.String()
				if financial != strings.Contains(html, `id="financial-access"`) {
					t.Fatal("requested scope not reflected")
				}
				if financial {
					for _, attribute := range []string{`for="financial-access"`, `aria-describedby="financial-help"`, `<option value="deny" selected>`} {
						if !strings.Contains(html, attribute) {
							t.Fatal("missing accessible independent default-denied financial consent", attribute)
						}
					}
				}
				if path := os.Getenv("CORES_MCP_CONSENT_PREVIEW_DIR"); path != "" && financial {
					if err := os.MkdirAll(path, 0700); err != nil {
						t.Fatal(err)
					}
					name := fmt.Sprintf("consent-%s-%t.html", lang, writes)
					if err := os.WriteFile(filepath.Join(path, name), out.Bytes(), 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
		}
	}
}
