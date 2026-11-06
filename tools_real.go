package main

// tools_real.go — implementasi tools dengan data SUNGGUHAN dari GCP.
//
// Aktif kalau env var TOOLS_MODE=real. Deklarasi tool (toolDeclarations) dan
// agentic loop di agent.go TIDAK berubah — yang diganti hanya sumber datanya:
//   list_services / check_service_health / get_deployment_status → Cloud Run Admin API v2
//   get_recent_logs                                              → Cloud Logging API v2
//
// Izin yang dibutuhkan service account (read-only):
//   roles/run.viewer, roles/logging.viewer
//
// Env var opsional:
//   GOOGLE_CLOUD_PROJECT — project yang diperiksa (default: project tempat Cloud Run berjalan)
//   REGION               — region Cloud Run (default: asia-southeast1)

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"cloud.google.com/go/compute/metadata"
	"golang.org/x/oauth2/google"
)

func realMode() bool { return os.Getenv("TOOLS_MODE") == "real" }

func gcpProject(ctx context.Context) (string, error) {
	if p := os.Getenv("GOOGLE_CLOUD_PROJECT"); p != "" {
		return p, nil
	}
	if metadata.OnGCE() { // juga true di Cloud Run
		return metadata.ProjectIDWithContext(ctx)
	}
	return "", fmt.Errorf("set GOOGLE_CLOUD_PROJECT")
}

func gcpRegion() string {
	if r := os.Getenv("REGION"); r != "" {
		return r
	}
	return "asia-southeast1"
}

// callGCP memanggil REST API Google dengan Application Default Credentials:
// di Cloud Run otomatis memakai service account service; di laptop memakai
// `gcloud auth application-default login`.
func callGCP(ctx context.Context, method, url string, body any) (map[string]any, error) {
	client, err := google.DefaultClient(ctx, "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return nil, fmt.Errorf("credentials: %w", err)
	}
	var reader io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s %s: HTTP %d: %s", method, url, resp.StatusCode, truncate(string(raw), 300))
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

func str(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

func obj(m map[string]any, key string) map[string]any {
	v, _ := m[key].(map[string]any)
	return v
}

func errResult(err error) map[string]any { return map[string]any{"error": err.Error()} }

// ---- Cloud Run Admin API v2 ----

func runServiceURL(ctx context.Context, name string) (string, error) {
	p, err := gcpProject(ctx)
	if err != nil {
		return "", err
	}
	u := fmt.Sprintf("https://run.googleapis.com/v2/projects/%s/locations/%s/services", p, gcpRegion())
	if name != "" {
		u += "/" + name
	}
	return u, nil
}

// serviceStatus menerjemahkan kondisi Cloud Run ke status sederhana yang dipahami agent.
func serviceStatus(svc map[string]any) string {
	cond := obj(svc, "terminalCondition")
	switch {
	case str(cond, "state") != "CONDITION_SUCCEEDED":
		return "unhealthy"
	case str(svc, "latestCreatedRevision") != str(svc, "latestReadyRevision"):
		return "degraded" // revision terbaru gagal ready; traffic masih di revision lama
	default:
		return "healthy"
	}
}

func realListServices(ctx context.Context) map[string]any {
	u, err := runServiceURL(ctx, "")
	if err != nil {
		return errResult(err)
	}
	resp, err := callGCP(ctx, "GET", u, nil)
	if err != nil {
		return errResult(err)
	}
	result := map[string]any{}
	items, _ := resp["services"].([]any)
	for _, it := range items {
		svc, _ := it.(map[string]any)
		result[path.Base(str(svc, "name"))] = map[string]any{
			"status":          serviceStatus(svc),
			"latest_revision": path.Base(str(svc, "latestReadyRevision")),
			"region":          gcpRegion(),
		}
	}
	return map[string]any{"services": result}
}

func realCheckServiceHealth(ctx context.Context, name string) map[string]any {
	u, err := runServiceURL(ctx, name)
	if err != nil {
		return errResult(err)
	}
	svc, err := callGCP(ctx, "GET", u, nil)
	if err != nil {
		return errResult(err)
	}
	cond := obj(svc, "terminalCondition")
	return map[string]any{
		"service":                 name,
		"status":                  serviceStatus(svc),
		"condition_state":         str(cond, "state"),
		"condition_message":       str(cond, "message"),
		"latest_ready_revision":   path.Base(str(svc, "latestReadyRevision")),
		"latest_created_revision": path.Base(str(svc, "latestCreatedRevision")),
		"url":                     str(svc, "uri"),
		"updated_at":              str(svc, "updateTime"),
	}
}

func realGetDeploymentStatus(ctx context.Context, name string) map[string]any {
	u, err := runServiceURL(ctx, name)
	if err != nil {
		return errResult(err)
	}
	svc, err := callGCP(ctx, "GET", u, nil)
	if err != nil {
		return errResult(err)
	}
	var traffic []string
	ts, _ := svc["trafficStatuses"].([]any)
	for _, t := range ts {
		m, _ := t.(map[string]any)
		rev := path.Base(str(m, "revision"))
		if rev == "." || rev == "" {
			rev = "latest"
		}
		traffic = append(traffic, fmt.Sprintf("%v%% → %s", m["percent"], rev))
	}
	tmpl := obj(svc, "template")
	var image string
	if cs, _ := tmpl["containers"].([]any); len(cs) > 0 {
		c, _ := cs[0].(map[string]any)
		image = str(c, "image")
	}
	return map[string]any{
		"service":          name,
		"current_revision": path.Base(str(svc, "latestReadyRevision")),
		"image":            image,
		"deployed_at":      str(svc, "updateTime"),
		"traffic":          strings.Join(traffic, ", "),
		"status":           serviceStatus(svc),
	}
}

// ---- Cloud Logging API v2 ----

func realGetRecentLogs(ctx context.Context, name, severity string) map[string]any {
	p, err := gcpProject(ctx)
	if err != nil {
		return errResult(err)
	}
	since := time.Now().Add(-1 * time.Hour).UTC().Format(time.RFC3339)
	filter := fmt.Sprintf(`resource.type="cloud_run_revision" AND resource.labels.service_name=%q AND timestamp>=%q`, name, since)
	switch strings.ToUpper(severity) {
	case "ERROR", "WARNING", "WARN", "INFO":
		sev := strings.ToUpper(severity)
		if sev == "WARN" {
			sev = "WARNING"
		}
		filter += " AND severity=" + sev
	}
	resp, err := callGCP(ctx, "POST", "https://logging.googleapis.com/v2/entries:list", map[string]any{
		"resourceNames": []string{"projects/" + p},
		"filter":        filter,
		"orderBy":       "timestamp desc",
		"pageSize":      20,
	})
	if err != nil {
		return errResult(err)
	}
	var entries []map[string]any
	items, _ := resp["entries"].([]any)
	for _, it := range items {
		e, _ := it.(map[string]any)
		msg := str(e, "textPayload")
		if msg == "" {
			if jp := obj(e, "jsonPayload"); jp != nil {
				msg = str(jp, "message")
			}
		}
		if msg == "" {
			if hr := obj(e, "httpRequest"); hr != nil {
				msg = fmt.Sprintf("HTTP %v %s %s", hr["status"], str(hr, "requestMethod"), str(hr, "requestUrl"))
			}
		}
		entries = append(entries, map[string]any{
			"severity":  str(e, "severity"),
			"message":   truncate(msg, 300),
			"timestamp": str(e, "timestamp"),
		})
	}
	return map[string]any{"service": name, "window": "1 jam terakhir", "logs": entries}
}

func executeRealTool(ctx context.Context, name string, args map[string]any) map[string]any {
	svc, _ := args["service_name"].(string)
	switch name {
	case "list_services":
		return realListServices(ctx)
	case "check_service_health":
		return realCheckServiceHealth(ctx, svc)
	case "get_recent_logs":
		sev, _ := args["severity"].(string)
		return realGetRecentLogs(ctx, svc, sev)
	case "get_deployment_status":
		return realGetDeploymentStatus(ctx, svc)
	default:
		return map[string]any{"error": "unknown tool: " + name}
	}
}
