package inspector

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/Daviey/bulwarkai/internal/config"
	"github.com/Daviey/bulwarkai/internal/translate"
)

type dlpInspector struct {
	project       string
	location      string
	infoTypes     []string
	minLikelihood string
	client        *http.Client
	endpoint      string
}

func NewDLPInspector(cfg *config.Config, httpClient *http.Client) *dlpInspector {
	infoTypes := config.SplitEnv("DLP_INFO_TYPES", "US_SOCIAL_SECURITY_NUMBER,CREDIT_CARD_NUMBER,EMAIL_ADDRESS,PHONE_NUMBER,PERSON_NAME,STREET_ADDRESS,DATE_OF_BIRTH")
	endpoint := os.Getenv("DLP_ENDPOINT")
	if endpoint == "" {
		endpoint = "https://dlp.googleapis.com"
	}
	return &dlpInspector{
		project:       cfg.Project,
		location:      config.EnvOr("DLP_LOCATION", cfg.Location),
		infoTypes:     infoTypes,
		minLikelihood: config.EnvOr("DLP_MIN_LIKELIHOOD", "LIKELY"),
		client:        httpClient,
		endpoint:      endpoint,
	}
}

func (d *dlpInspector) Name() string { return "dlp" }

func (d *dlpInspector) TestMethod() string { return TestSSN }

func (d *dlpInspector) InspectPrompt(ctx context.Context, text string, token string) *BlockResult {
	return d.inspect(ctx, text, token)
}

func (d *dlpInspector) InspectResponse(ctx context.Context, text string, token string) *BlockResult {
	return d.inspect(ctx, text, token)
}

func (d *dlpInspector) inspect(ctx context.Context, text, token string) *BlockResult {
	var infoTypeConfigs []interface{}
	for _, it := range d.infoTypes {
		infoTypeConfigs = append(infoTypeConfigs, map[string]interface{}{"name": it})
	}
	payload := map[string]interface{}{
		"item": map[string]interface{}{"value": text},
		"inspectConfig": map[string]interface{}{
			"infoTypes":     infoTypeConfigs,
			"minLikelihood": d.minLikelihood,
			"limits":        map[string]interface{}{"maxFindingsPerRequest": 10},
		},
	}
	body, _ := json.Marshal(payload)
	url := fmt.Sprintf("%s/v2/projects/%s/locations/%s/content:inspect", d.endpoint, d.project, d.location)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		slog.Error("dlp request error", "error", err)
		return nil
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := d.client.Do(req)
	if err != nil {
		slog.Error("dlp call error", "error", err)
		return nil
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		slog.Warn("dlp non-ok response", "status", resp.StatusCode, "body", string(data[:min(len(data), 200)]))
		return nil
	}
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		slog.Error("dlp response decode error", "error", err)
		return nil
	}
	findings, _ := result["result"].(map[string]interface{})["findings"].([]interface{})
	if len(findings) == 0 {
		return nil
	}
	var types []string
	seen := map[string]bool{}
	for _, f := range findings {
		if finding, ok := f.(map[string]interface{}); ok {
			if infoType, ok := finding["infoType"].(map[string]interface{}); ok {
				name := translate.StrVal(infoType["name"], "unknown")
				if !seen[name] {
					seen[name] = true
					types = append(types, name)
				}
			}
		}
	}
	if len(types) > 0 {
		return &BlockResult{Blocked: true, Reason: "DLP: " + strings.Join(types, ", ")}
	}
	return nil
}
