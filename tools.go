package main

import (
	"fmt"
	"time"

	"google.golang.org/genai"
)

// Data simulasi — di produksi, ini diganti dengan GCP API calls.
var services = map[string]*serviceData{
	"payment-api": {
		status:     "healthy",
		revision:   "payment-api-00012-abc",
		deployedAt: time.Now().Add(-2 * time.Hour),
		instances:  3,
		region:     "asia-southeast1",
	},
	"user-service": {
		status:     "degraded",
		revision:   "user-service-00008-xyz",
		deployedAt: time.Now().Add(-30 * time.Minute),
		instances:  1,
		region:     "asia-southeast1",
	},
	"notification-worker": {
		status:     "unhealthy",
		revision:   "notification-worker-00003-qrs",
		deployedAt: time.Now().Add(-5 * time.Minute),
		instances:  0,
		region:     "asia-southeast1",
	},
}

var mockLogs = map[string][]logEntry{
	"payment-api": {
		{severity: "INFO", message: "Request processed in 45ms", timestamp: time.Now().Add(-1 * time.Minute)},
		{severity: "INFO", message: "Request processed in 52ms", timestamp: time.Now().Add(-2 * time.Minute)},
	},
	"user-service": {
		{severity: "ERROR", message: "dial tcp: connection refused (database)", timestamp: time.Now().Add(-28 * time.Minute)},
		{severity: "ERROR", message: "dial tcp: connection refused (database)", timestamp: time.Now().Add(-29 * time.Minute)},
		{severity: "WARN", message: "Retrying database connection (attempt 3/5)", timestamp: time.Now().Add(-30 * time.Minute)},
	},
	"notification-worker": {
		{severity: "ERROR", message: "GEMINI_API_KEY not set — agent cannot start", timestamp: time.Now().Add(-5 * time.Minute)},
		{severity: "ERROR", message: "Container failed health check on port 8080", timestamp: time.Now().Add(-4 * time.Minute)},
	},
}

type serviceData struct {
	status     string
	revision   string
	deployedAt time.Time
	instances  int
	region     string
}

type logEntry struct {
	severity  string
	message   string
	timestamp time.Time
}

func listServices() map[string]any {
	result := map[string]any{}
	for name, s := range services {
		result[name] = map[string]any{
			"status":    s.status,
			"instances": s.instances,
			"region":    s.region,
		}
	}
	return map[string]any{"services": result}
}

func checkServiceHealth(serviceName string) map[string]any {
	s, ok := services[serviceName]
	if !ok {
		return map[string]any{
			"error": fmt.Sprintf("service '%s' not found. Available: payment-api, user-service, notification-worker", serviceName),
		}
	}
	return map[string]any{
		"service":     serviceName,
		"status":      s.status,
		"revision":    s.revision,
		"instances":   s.instances,
		"region":      s.region,
		"deployed_at": s.deployedAt.Format(time.RFC3339),
	}
}

func getRecentLogs(serviceName string, severity string) map[string]any {
	logs, ok := mockLogs[serviceName]
	if !ok {
		return map[string]any{"error": fmt.Sprintf("service '%s' not found", serviceName)}
	}

	var entries []map[string]any
	for _, l := range logs {
		if severity == "" || l.severity == severity {
			entries = append(entries, map[string]any{
				"severity":  l.severity,
				"message":   l.message,
				"timestamp": l.timestamp.Format(time.RFC3339),
			})
		}
	}
	return map[string]any{"service": serviceName, "logs": entries}
}

func getDeploymentStatus(serviceName string) map[string]any {
	s, ok := services[serviceName]
	if !ok {
		return map[string]any{"error": fmt.Sprintf("service '%s' not found", serviceName)}
	}
	return map[string]any{
		"service":          serviceName,
		"current_revision": s.revision,
		"deployed_at":      s.deployedAt.Format(time.RFC3339),
		"traffic":          "100% → " + s.revision,
		"status":           s.status,
	}
}

var toolDeclarations = &genai.Tool{
	FunctionDeclarations: []*genai.FunctionDeclaration{
		{
			Name:        "list_services",
			Description: "List semua Cloud Run services beserta status dan jumlah instance yang berjalan.",
		},
		{
			Name:        "check_service_health",
			Description: "Cek status kesehatan detail sebuah Cloud Run service: status, revision aktif, jumlah instance, region.",
			Parameters: &genai.Schema{
				Type: genai.TypeObject,
				Properties: map[string]*genai.Schema{
					"service_name": {Type: genai.TypeString, Description: "Nama Cloud Run service"},
				},
				Required: []string{"service_name"},
			},
		},
		{
			Name:        "get_recent_logs",
			Description: "Ambil log terbaru dari sebuah service. Gunakan untuk menginvestigasi error atau perilaku tak terduga.",
			Parameters: &genai.Schema{
				Type: genai.TypeObject,
				Properties: map[string]*genai.Schema{
					"service_name": {Type: genai.TypeString, Description: "Nama Cloud Run service"},
					"severity":     {Type: genai.TypeString, Description: "Filter severity: ERROR, WARN, INFO. Kosongkan untuk semua."},
				},
				Required: []string{"service_name"},
			},
		},
		{
			Name:        "get_deployment_status",
			Description: "Lihat status deployment terbaru sebuah service: revision aktif, waktu deploy, dan traffic split.",
			Parameters: &genai.Schema{
				Type: genai.TypeObject,
				Properties: map[string]*genai.Schema{
					"service_name": {Type: genai.TypeString, Description: "Nama Cloud Run service"},
				},
				Required: []string{"service_name"},
			},
		},
	},
}

func executeTool(name string, args map[string]any) map[string]any {
	switch name {
	case "list_services":
		return listServices()
	case "check_service_health":
		svc, _ := args["service_name"].(string)
		return checkServiceHealth(svc)
	case "get_recent_logs":
		svc, _ := args["service_name"].(string)
		sev, _ := args["severity"].(string)
		return getRecentLogs(svc, sev)
	case "get_deployment_status":
		svc, _ := args["service_name"].(string)
		return getDeploymentStatus(svc)
	default:
		return map[string]any{"error": "unknown tool: " + name}
	}
}
