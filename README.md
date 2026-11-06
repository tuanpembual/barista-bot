# Barista Bot Ops
## Deploying and Containerizing an Autonomous Agent on Cloud Run

**Track:** Workshop 2 (Cloud) · GDG  
**Durasi:** 50 menit  
**Level:** Intermediate  
**Deliverable:** URL publik Cloud Run + Dockerfile + cloudbuild.yaml

---

## Kenapa Bangun Sendiri?

Banyak dari kita sudah memakai asisten AI setiap hari — Gemini, ChatGPT, Claude, atau Copilot. Di workshop ini kita fokus ke Gemini: chat di [gemini.google.com](https://gemini.google.com), Gemini di Gmail dan Docs, atau Gemini Code Assist di VS Code untuk bantu baca kode dan debug. Jadi pertanyaan yang wajar:

> *"Ngapain susah-susah build agent sendiri kalau Gemini sudah bisa semua itu?"*

Workshop ini tetap pakai Gemini — tapi bukan Gemini yang biasa kita ajak chat. Yang kita pakai adalah **Gemini API**: otak yang sama, tapi kita pasang sebagai engine di dalam service kita sendiri. Perbedaan ini yang jadi inti dari sesi ini.

Jawabannya bukan soal kemampuan model — modelnya sama. Ini soal tiga hal yang tidak bisa dilakukan aplikasi chat, karena memang tidak dirancang untuk itu.

### 1. Aplikasi chat menunggu pertanyaan dari pengguna

Gemini app dan Gemini Code Assist menunggu pengguna mengetik. Sistem lain tidak bisa memanggilnya secara otomatis.

Barista Bot adalah HTTP service. Ia bisa dipanggil dari:
- Alert monitoring saat service down
- Slack slash command dari channel ops
- Cron job setiap jam untuk daily health report
- GitHub Actions setelah deployment selesai

Tidak perlu ada orang yang duduk dan mengetik.

### 2. Aplikasi chat tidak punya akses ke sistem produksi

Gemini Code Assist bisa membaca file di laptop karena extension-nya terpasang di sana. Tapi ia tidak bisa memanggil Cloud Monitoring API di lingkungan produksi, membaca log dari Cloud Logging, atau query database internal — kecuali semua itu diekspos keluar, yang bukan ide bagus.

Agent yang di-deploy di GCP project sendiri bisa diberi service account dengan akses ke sistem internal. Data internal (log, metrics, credentials) tidak perlu keluar dari GCP project tersebut — yang dikirim ke Gemini API hanya pertanyaan dan hasil tool yang relevan, bukan raw data infrastruktur.

### 3. Aplikasi chat tidak bisa dipakai tim secara shared

Setiap orang membuka chat-nya sendiri, dengan prompt dan konteks masing-masing. Kalau mau automasi yang sama dipakai 10 engineer, diintegrasikan ke Slack workspace, atau dipanggil dari dashboard internal — dibutuhkan satu endpoint yang bisa di-share.

Satu URL Cloud Run. Semua orang bisa pakai, dengan perilaku yang sama.

---

**Intinya bukan "ganti Gemini app dengan Barista Bot".** Pakai sesuai konteks: Gemini app dan Code Assist untuk eksplorasi dan kerja harian; **agent buatan sendiri dengan Gemini API** untuk pekerjaan yang harus jalan otomatis, terhubung ke sistem internal, dan dipakai bersama tim.

> 💡 Bagi yang pernah melihat tool otomasi visual (no-code) yang punya blok "AI Agent" — misalnya n8n — konsep di dalamnya sama dengan yang kita bangun hari ini. Bedanya, di sini kita menulisnya sendiri, jadi setiap langkahnya terlihat jelas.

---

## Apa yang akan dibangun

**Barista Bot** — AI agent yang menerima pertanyaan ops dalam bahasa natural, investigasi status layanan secara otomatis, dan memberikan ringkasan + rekomendasi — berjalan sebagai microservice di Google Cloud Run.

```
Operator
  │
  ▼
POST /ask {"message": "Ada service yang bermasalah?"}
  │
  ▼
Cloud Run (Go net/http)
  │
  ▼
Gemini API ──── function_call: list_services()
  │                    │
  │◄──── result: {user-service: degraded, notification-worker: unhealthy}
  │
  ├──── function_call: get_recent_logs("user-service", "ERROR")
  │                    │
  │◄──── result: {"dial tcp: connection refused (database)"}
  │
  ▼
{"reply": "Dua service bermasalah: user-service degraded karena DB connection refused, 
           notification-worker unhealthy (GEMINI_API_KEY tidak di-set). 
           Prioritas: cek koneksi database di user-service."}
```

**Stack:**
- Go 1.27 + net/http (stdlib, tanpa framework tambahan)
- Google Gemini API (`google.golang.org/genai`) — function calling
- Docker multi-stage build
- Google Cloud Run + Artifact Registry
- Cloud Build (CI/CD)

> Kenapa Go bukan Python? Cold start lebih cepat, single binary, image lebih kecil (~20MB vs ~200MB).
> Di Cloud Run yang scale-to-zero, ini penting untuk pengalaman end-user.

---

## Konsep Dasar

### 1. Apa itu AI Agent?

Chatbot biasa bekerja seperti ini: pengguna mengirim pesan → model jawab → selesai. Satu round trip.

**Agent** berbeda. Agent bisa mengambil tindakan berulang sampai tugasnya selesai:

```
Operator: "Ada service yang bermasalah?"
  │
  ▼
Agent berpikir: "Saya perlu cek semua service dulu"
  → panggil list_services()                       ← tindakan 1
  → temukan user-service: degraded
  → panggil get_recent_logs("user-service", "ERROR")  ← tindakan 2
  → temukan: "connection refused (database)"
  ▼
Agent menjawab: "user-service degraded karena DB connection refused sejak 30 menit lalu"
```

Kunci perbedaannya: agent bisa **memutuskan sendiri** tools apa yang perlu dipanggil, berapa kali, dan dalam urutan apa — berdasarkan konteks percakapan.

---

### 2. Function Calling / Tool Use

Ini adalah mekanisme yang memungkinkan model bahasa berinteraksi dengan dunia luar.

Caranya: saat mengirim pesan ke Gemini, kita juga menyertakan **daftar tools** yang tersedia — nama fungsinya, deskripsinya, dan parameter yang dibutuhkan. Model kemudian memutuskan apakah perlu memanggil tool tersebut.

```
Kita mengirim ke Gemini:
  message: "Cek status user-service"
  tools: [list_services, check_service_health, get_recent_logs, get_deployment_status]
                │
                ▼
Gemini memutuskan: "Ini pertanyaan tentang health service tertentu."
Gemini merespons bukan dengan teks, tapi dengan:
  function_call: { name: "check_service_health", args: {"service_name": "user-service"} }
                │
                ▼
Kode kita mengeksekusi checkServiceHealth("user-service") di server
Hasil dikirim balik ke Gemini
                │
                ▼
Gemini baru menjawab dengan teks: "user-service dalam kondisi degraded. Revision aktif..."
```

Model **tidak mengeksekusi kode** — ia hanya bilang "tolong panggil fungsi ini dengan argumen ini". Eksekusinya ada di kode kita. Ini penting untuk keamanan: kita yang mengontrol apa yang boleh dan tidak boleh dilakukan.

---

### 3. Apa yang Membuat Ini Autonomous Agent?

Perbedaan mendasar antara "API yang pakai LLM" dengan "agent" ada di kemampuan memutuskan sendiri — tool apa yang dipanggil, dalam urutan apa, berdasarkan konteks.

Contoh: operator mengirim `"Kenapa notification-worker tidak jalan?"`

```
1. Understand  → model memahami ini investigasi service down
2. Choose      → pilih check_service_health("notification-worker") dulu
3. Execute     → instances: 0, status: unhealthy
4. Choose lagi → perlu tahu kenapa, pilih get_recent_logs("notification-worker", "ERROR")
5. Execute     → "GEMINI_API_KEY not set — agent cannot start"
6. Respond     → "notification-worker down karena env var GEMINI_API_KEY tidak dikonfigurasi"
```

Model tidak perlu di-instruksikan "kalau service down, cek log". Ia memutuskan sendiri berdasarkan deskripsi tool yang kita tulis di `toolDeclarations`.

---

### 4. Agentic Loop

Agentic loop adalah pola "jalankan → cek → lanjut" yang diulang sampai tidak ada lagi function call:

```
send_message(user_input)
    │
    ▼
response dari Gemini
    │
    ├─ ada function_call? → eksekusi → kirim hasil → kembali ke atas
    │
    └─ tidak ada? → ambil teks final → selesai
```

Di kode `agent.go` kita, loop ini dibatasi 5 iterasi (`for range 5`) untuk menghindari infinite loop kalau ada bug. Untuk pertanyaan ops sederhana, biasanya 2–3 iterasi sudah cukup.

---

### 5. Kenapa Cloud Run?

Ada banyak cara deploy aplikasi di GCP. Perbandingan singkat:

| | Cloud Run | App Engine | GKE (Kubernetes) | Compute Engine (VM) |
|---|---|---|---|---|
| **Complexity** | Rendah | Rendah | Tinggi | Menengah |
| **Scaling** | Otomatis (0 → N) | Otomatis | Manual/auto | Manual |
| **Billing** | Per request | Per instance | Per node | Per jam |
| **Cold start** | Ada (~1–3 detik) | Ada | Tidak | Tidak |
| **Cocok untuk** | Stateless API, event-driven | Web app standar | Microservices besar | Kontrol penuh |

Cloud Run cocok untuk agent ops karena:
- **Scale to zero**: tidak bayar saat tidak ada traffic (gratis saat idle)
- **Deploy cukup push image**: tidak perlu manage server atau cluster
- **Stateless by design**: setiap request independen — ini sesuai dengan arsitektur agent kita

---

### 6. Kontainerisasi

Container adalah cara membungkus aplikasi beserta semua dependensinya ke dalam satu unit yang bisa dijalankan di mana saja secara konsisten.

```
Tanpa container:                    Dengan container:
  "Di laptopku jalan..."              [Go 1.27]
  "Di server kok error?"              [google.golang.org/genai]
                                      [kode aplikasi]
  → versi Go beda                     [env vars]
  → library tidak terinstall          ─────────────────────
  → path berbeda                      Sama persis di mana saja
```

**Dockerfile** adalah resep untuk membuat container. Workshop ini menggunakan **multi-stage build** — binary Go dikompilasi di image builder yang besar, lalu hanya binary-nya yang disalin ke image runtime yang kecil (~20MB):

```dockerfile
FROM golang:1.27-alpine AS builder   # image build: ~300MB
RUN go build -o app .

FROM alpine:3.20                     # image runtime: ~20MB
COPY --from=builder /app/app .
CMD ["./app"]
```

---

### 7. Istilah Penting

**`tools.go`**
File yang berisi "kemampuan" agent. Isinya fungsi-fungsi Go biasa — `listServices`, `checkServiceHealth`, `getRecentLogs`, `getDeploymentStatus` — yang benar-benar dijalankan di server saat agent butuh informasi. Di workshop ini fungsinya mengembalikan data simulasi; di produksi, isinya diganti dengan panggilan ke Cloud Logging atau Cloud Monitoring API.

**`toolDeclarations`**
"Daftar menu" yang dikirim ke Gemini bersama setiap pesan: nama tiap tool, deskripsinya, dan parameter yang dibutuhkan. Gemini tidak bisa menjalankan kode — ia hanya membaca daftar ini lalu membalas dengan permintaan seperti *"jalankan `get_recent_logs` dengan `service_name=user-service`"*. Permintaan itu kemudian dieksekusi oleh fungsi `executeTool` di server kita.

> Analogi: `toolDeclarations` adalah **buku menu**, fungsi di `tools.go` adalah **dapur**. Gemini memesan dari menu, kode kita yang memasak. Karena Gemini memilih tool berdasarkan deskripsi, **kualitas deskripsi tool sangat menentukan** apakah agent memanggil tool yang tepat.

**RAG (Retrieval-Augmented Generation)**
Teknik di mana AI **mencari dokumen yang relevan terlebih dahulu**, lalu menyusun jawaban berdasarkan dokumen tersebut. Tujuannya agar jawaban berdasar fakta dari sumber yang tersedia (misalnya dokumentasi internal), bukan karangan model. Contoh: bot support yang mencari halaman dokumentasi dulu, lalu menjawab sambil menyertakan link referensinya. Barista Bot tidak memakai RAG — datanya diambil langsung lewat tools — tapi RAG bisa ditambahkan sebagai salah satu tool (misalnya `search_runbook`).

---

## Prasyarat

Siapkan **sebelum hari H** (paling lambat H-1):

- **Akun Google pribadi (Gmail)** — hindari akun kantor/kampus/sekolah. Akun organisasi sering kena org policy yang memblokir `--allow-unauthenticated` atau pembuatan service account key.
- **GCP project baru dengan billing aktif** — billing butuh kartu kredit/debit. Kalau panitia membagikan **credit voucher**, redeem dulu sebelum hari H. Free tier cukup; biaya workshop praktis $0 (lihat Estimasi Biaya).
- **Gemini API key milik sendiri** → [aistudio.google.com](https://aistudio.google.com). Wajib per peserta — jangan mengandalkan shared key.

### Pilih environment

**Opsi A — Cloud Shell (direkomendasikan).** Buka [shell.cloud.google.com](https://shell.cloud.google.com). gcloud, Docker, dan Go sudah terpasang dan sudah login. Tidak perlu install apa-apa, tidak terpengaruh Wi-Fi venue untuk push image, dan sudah `linux/amd64`.

> Cek versi Go di Cloud Shell dengan `go version`. Kalau di bawah 1.27, pakai `GOTOOLCHAIN=auto` (default) — Go akan otomatis mengunduh toolchain sesuai `go.mod`.

**Opsi B — Laptop sendiri.**

```bash
gcloud --version   # >= 450.0.0
docker --version   # >= 24.0
go version         # >= 1.27
```

> Kalau Docker di laptop bermasalah, lewati build lokal di Bagian 2 dan langsung pakai Cloud Build di Bagian 4 (build berjalan di cloud).

---

## Setup (5 menit)

### Clone starter repo

Kode lengkap sudah tersedia — tidak perlu mengetik dari nol.

```bash
git clone https://github.com/tuanpembual/barista-bot.git
cd barista-bot
```

### Environment variables

```bash
export PROJECT_ID="<project-id-anda>"   # lihat: gcloud projects list
export REGION="asia-southeast1"
export GEMINI_API_KEY="<api-key-anda>"  # jangan di-commit, jangan di-paste ke chat/slide
```

> API key dibuat di [aistudio.google.com](https://aistudio.google.com) → **Get API key** → **Create API key** → pilih GCP project.
>
> Kalau key sudah tersimpan di Secret Manager (setelah Bagian 3, atau saat membuka terminal baru), ambil dari sana tanpa perlu copy-paste:
>
> ```bash
> export GEMINI_API_KEY=$(gcloud secrets versions access latest --secret=gemini-api-key)
> ```

> ⚠️ Ganti `PROJECT_ID` dengan project milik sendiri. Semua perintah dan `cloudbuild.yaml` membaca project dari sini / dari `gcloud config`, tidak ada project ID yang di-hardcode.

### Login dan set project

```bash
gcloud auth login            # lewati kalau pakai Cloud Shell
gcloud config set project $PROJECT_ID
```

Ada dua jenis login di gcloud, dan keduanya berbeda:

| Perintah | Dipakai oleh | Kapan dibutuhkan |
|---|---|---|
| `gcloud auth login` | perintah `gcloud` di terminal | Semua bagian workshop |
| `gcloud auth application-default login` | **aplikasi/kode** yang memanggil Google API (ADC — Application Default Credentials) | Hanya untuk menjalankan mode real di lokal (bagian *Setelah Workshop*) |

Untuk menyiapkan ADC sekalian (opsional):

```bash
gcloud auth application-default login
gcloud auth application-default set-quota-project $PROJECT_ID
```

> ⚠️ `application-default login` **menimpa** ADC yang sudah ada di laptop. Kalau laptop juga dipakai untuk pekerjaan lain dengan akun berbeda, simpan ADC workshop di folder terpisah:
>
> ```bash
> CLOUDSDK_CONFIG=$HOME/.config/gcloud-workshop gcloud auth application-default login
> export GOOGLE_APPLICATION_CREDENTIALS=$HOME/.config/gcloud-workshop/application_default_credentials.json
> ```
>
> Di Cloud Shell, ADC sudah otomatis tersedia — langkah ini bisa dilewati.

### Enable GCP APIs

```bash
gcloud services enable \
  run.googleapis.com \
  cloudbuild.googleapis.com \
  artifactregistry.googleapis.com \
  secretmanager.googleapis.com \
  --project=$PROJECT_ID
```

> APIs yang dibutuhkan: Cloud Run, Cloud Build, Artifact Registry, Secret Manager.
> Cukup dijalankan sekali per project.

### Buat Artifact Registry repo

```bash
gcloud artifacts repositories create barista-bot \
  --repository-format=docker \
  --location=$REGION \
  --project=$PROJECT_ID
```

---

## Bagian 1 — Build the Agent (15 menit)

> Bagian ini adalah **walkthrough kode**, bukan sesi mengetik. Semua file di bawah sudah ada di starter repo. Trainer menjelaskan alurnya, peserta membaca file di editor lalu langsung ke **Test lokal**. Kalau ada waktu, coba tambah satu tool baru di `tools.go`.

### Struktur project

```
barista-bot/
├── main.go        ← HTTP server (net/http)
├── agent.go       ← Gemini agentic loop
├── tools.go       ← Tool definitions + simulated cloud data
├── tools_real.go  ← (opsional) tools dengan data GCP sungguhan — lihat bagian "Mode Real"
├── go.mod
├── go.sum
├── Dockerfile
├── .dockerignore
└── cloudbuild.yaml
```

<details>
<summary>Mau mulai dari nol? (opsional, di luar alokasi waktu)</summary>

```bash
mkdir barista-bot && cd barista-bot
go mod init barista-bot
go get google.golang.org/genai@latest
```

</details>

---

### tools.go

Tools yang tersedia untuk agent: list services, cek health, baca log, lihat deployment status.
Data di sini disimulasikan — di produksi, ini diganti dengan GCP API calls (Cloud Monitoring, Cloud Logging).

```go
package main

import (
	"context"
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

// executeTool menjalankan tool yang diminta Gemini.
// TOOLS_MODE=real → data sungguhan dari GCP (lihat tools_real.go); selain itu → data simulasi di atas.
func executeTool(ctx context.Context, name string, args map[string]any) map[string]any {
	if realMode() {
		return executeRealTool(ctx, name, args)
	}
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
```

---

### agent.go

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"google.golang.org/genai"
)

const systemPrompt = `Kamu adalah Cloud Ops Agent, asisten monitoring dan observabilitas untuk layanan Cloud Run.

Tugasmu:
- Investigasi kesehatan service berdasarkan pertanyaan operator
- Gunakan tools untuk list service, cek health, ambil log, dan status deployment
- Setelah investigasi, berikan ringkasan situasi dan rekomendasi tindakan

Workflow standar:
1. Jika tidak tahu service mana yang bermasalah, panggil list_services dulu
2. Cek health service yang mencurigakan dengan check_service_health
3. Jika ada anomali, ambil log error dengan get_recent_logs
4. Rangkum temuan dan sarankan langkah perbaikan

Gaya bicara: singkat, teknikal, dan actionable. Bahasa Indonesia.`

// Model bisa diganti lewat env var GEMINI_MODEL (mis. model "lite" yang kuota free tier-nya lebih besar).
func modelName() string {
	if m := os.Getenv("GEMINI_MODEL"); m != "" {
		return m
	}
	return "gemini-3.8-flash"
}

// generateWithRetry: retry dengan backoff kalau kena rate limit (HTTP 429) atau 503,
// sering terjadi kalau banyak peserta memakai API key / project yang sama.
func generateWithRetry(ctx context.Context, client *genai.Client, contents []*genai.Content, config *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error) {
	backoff := 2 * time.Second
	for attempt := 1; ; attempt++ {
		resp, err := client.Models.GenerateContent(ctx, modelName(), contents, config)
		var apiErr genai.APIError
		if err == nil || attempt == 4 || !errors.As(err, &apiErr) || (apiErr.Code != 429 && apiErr.Code != 503) {
			return resp, err
		}
		log.Printf("gemini %d, retry %d dalam %s", apiErr.Code, attempt, backoff)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff):
		}
		backoff *= 2
	}
}

func runAgent(ctx context.Context, userMessage string) (string, error) {
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  os.Getenv("GEMINI_API_KEY"),
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return "", fmt.Errorf("init client: %w", err)
	}

	config := &genai.GenerateContentConfig{
		SystemInstruction: genai.NewContentFromText(systemPrompt, "user"),
		Tools:             []*genai.Tool{toolDeclarations},
	}

	contents := []*genai.Content{
		genai.NewContentFromText(userMessage, "user"),
	}

	// Agentic loop: maksimal 5 iterasi
	for range 5 {
		resp, err := generateWithRetry(ctx, client, contents, config)
		if err != nil {
			return "", fmt.Errorf("generate: %w", err)
		}

		calls := resp.FunctionCalls()

		// Tidak ada function call → ambil teks final
		if len(calls) == 0 {
			for _, part := range resp.Candidates[0].Content.Parts {
				if part.Text != "" {
					return part.Text, nil
				}
			}
			return "", nil
		}

		// Tambahkan response model ke history
		contents = append(contents, resp.Candidates[0].Content)

		// Eksekusi semua tool calls, kirim hasilnya sekaligus
		var toolParts []*genai.Part
		for _, fc := range calls {
			log.Printf("tool call: %s %v", fc.Name, fc.Args)
			result := executeTool(ctx, fc.Name, fc.Args)
			toolParts = append(toolParts, genai.NewPartFromFunctionResponse(fc.Name, result))
		}
		contents = append(contents, genai.NewContentFromParts(toolParts, "user"))
	}

	return "", fmt.Errorf("max iterations reached")
}
```

---

### main.go

```go
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
)

type AskRequest struct {
	Message string `json:"message"`
}

type AskResponse struct {
	Reply string `json:"reply,omitempty"`
	Error string `json:"error,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	mux := http.NewServeMux()

	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "barista-bot"})
	})

	mux.HandleFunc("POST /ask", func(w http.ResponseWriter, r *http.Request) {
		var req AskRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Message == "" {
			writeJSON(w, http.StatusBadRequest, AskResponse{Error: "field 'message' required"})
			return
		}

		reply, err := runAgent(r.Context(), req.Message)
		if err != nil {
			log.Printf("agent error: %v", err)
			writeJSON(w, http.StatusInternalServerError, AskResponse{Error: err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, AskResponse{Reply: reply})
	})

	log.Printf("barista-bot listening on :%s", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatal(err)
	}
}
```

### Test lokal

```bash
export GEMINI_API_KEY="your-key-here"
# atau, kalau sudah tersimpan di Secret Manager:
# export GEMINI_API_KEY=$(gcloud secrets versions access latest --secret=gemini-api-key)

go run .

# Terminal baru
curl http://localhost:8080/

# Pertanyaan investigasi — agent akan otomatis list → cek health → baca log
curl -X POST http://localhost:8080/ask \
  -H "Content-Type: application/json" \
  -d '{"message": "Ada service yang bermasalah?"}'

# Pertanyaan spesifik
curl -X POST http://localhost:8080/ask \
  -H "Content-Type: application/json" \
  -d '{"message": "Kenapa notification-worker tidak bisa start?"}'
```

> ⏱️ Satu request `/ask` biasanya butuh **10–15 detik** — model dipanggil beberapa kali (tiap putaran tool call = satu panggilan ke Gemini). Ini normal, bukan hang.

Perhatikan output log di terminal pertama — akan terlihat baris `tool call: ...` untuk setiap tool yang dipanggil agent secara berurutan sebelum menjawab. Di Cloud Run, log yang sama muncul di **Cloud Run → barista-bot → Logs**.

---

## Bagian 2 — Containerize (10 menit)

### Dockerfile (multi-stage)

```dockerfile
# Stage 1: build binary
FROM golang:1.27-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o barista-bot .

# Stage 2: image minimal (~20MB)
FROM alpine:3.20
RUN adduser -D appuser
WORKDIR /app
COPY --from=builder /app/barista-bot .
USER appuser

EXPOSE 8080
CMD ["./barista-bot"]
```

### .dockerignore

```
barista-bot
.env
.git
```

### Build dan test container

```bash
# Test lokal (platform default sesuai laptop)
docker build -t barista-bot:local .

docker run --rm -p 8080:8080 \
  -e GEMINI_API_KEY=$GEMINI_API_KEY \
  barista-bot:local

curl http://localhost:8080/

# Build untuk push ke Cloud Run (harus amd64)
docker build --platform linux/amd64 -t barista-bot:amd64 .
```

> ⚠️ Mac M-series build untuk `linux/arm64` secara default. Cloud Run butuh `linux/amd64`.
> Selalu pakai `--platform linux/amd64` saat build image untuk di-push.

---

## Bagian 3 — Deploy ke Cloud Run (10 menit)

### Push image ke Artifact Registry

```bash
gcloud auth configure-docker $REGION-docker.pkg.dev

IMAGE="$REGION-docker.pkg.dev/$PROJECT_ID/barista-bot/app:v1"

docker tag barista-bot:amd64 $IMAGE
docker push $IMAGE
```

### Simpan API key di Secret Manager

```bash
echo -n "$GEMINI_API_KEY" | gcloud secrets create gemini-api-key \
  --data-file=- \
  --replication-policy=automatic
```

Cek bahwa secret tersimpan dengan benar — perintah ini juga cara mengambil kembali key di terminal baru:

```bash
export GEMINI_API_KEY=$(gcloud secrets versions access latest --secret=gemini-api-key)
echo ${#GEMINI_API_KEY}   # menampilkan panjang key saja, bukan isinya
```

> Kalau secret sudah ada (error `already exists`), tambahkan versi baru alih-alih membuat ulang:
> `echo -n "$GEMINI_API_KEY" | gcloud secrets versions add gemini-api-key --data-file=-`

### Beri akses Secret Manager ke Cloud Run

Cloud Run berjalan sebagai default Compute Engine service account. Akun ini perlu izin eksplisit untuk membaca secret.

```bash
PROJECT_NUMBER=$(gcloud projects describe $PROJECT_ID --format="value(projectNumber)")

gcloud secrets add-iam-policy-binding gemini-api-key \
  --project=$PROJECT_ID \
  --member="serviceAccount:${PROJECT_NUMBER}-compute@developer.gserviceaccount.com" \
  --role="roles/secretmanager.secretAccessor"
```

> Cukup dijalankan sekali. Tanpa ini, deploy akan gagal dengan error `Permission denied on secret`.

### Deploy

```bash
gcloud run deploy barista-bot \
  --image=$IMAGE \
  --region=$REGION \
  --allow-unauthenticated \
  --set-secrets="GEMINI_API_KEY=gemini-api-key:latest" \
  --memory=512Mi \
  --min-instances=0 \
  --max-instances=5 \
  --port=8080
```

### Test live

```bash
SERVICE_URL=$(gcloud run services describe barista-bot \
  --region=$REGION \
  --format="value(status.url)")

echo $SERVICE_URL

curl $SERVICE_URL/

curl -X POST $SERVICE_URL/ask \
  -H "Content-Type: application/json" \
  -d '{"message": "Cek semua service, ada yang bermasalah?"}'
```

### Lihat log agent secara live

Buka **terminal kedua** dan jalankan:

```bash
gcloud beta run services logs tail barista-bot --region=$REGION
```

Biarkan terminal ini terbuka, lalu kirim `curl` ke `/ask` dari terminal pertama. Baris `tool call: ...` akan muncul satu per satu saat agent memanggil tools — terlihat jelas agent memutuskan sendiri urutan investigasinya.

> - Saat pertama kali dijalankan, gcloud akan menawarkan instalasi komponen tambahan (`beta`, dan bisa juga `log-streaming`). Jawab `Y`. Di Cloud Shell, komponen ini biasanya sudah tersedia.
> - Log muncul dengan jeda beberapa detik — ini normal.
> - Tekan `Ctrl+C` untuk berhenti.
>
> Alternatif tanpa komponen beta (bukan live, menampilkan log terakhir):
> `gcloud run services logs read barista-bot --region=$REGION --limit=30`

---

## Bagian 4 — CI/CD dengan Cloud Build (5 menit)

### Beri izin Cloud Build untuk build, push, dan deploy

Di project yang dibuat sejak pertengahan 2024, Cloud Build secara default berjalan sebagai **Compute Engine default service account** (`PROJECT_NUMBER-compute@developer.gserviceaccount.com`), bukan legacy `PROJECT_NUMBER@cloudbuild.gserviceaccount.com`. Service account ini juga **tidak lagi otomatis dapat role Editor**, jadi semua izin perlu diberikan eksplisit.

Cek service account yang dipakai Cloud Build di project tersebut:

```bash
gcloud builds get-default-service-account --project=$PROJECT_ID
```

Beri izin ke service account tersebut:

```bash
PROJECT_NUMBER=$(gcloud projects describe $PROJECT_ID --format="value(projectNumber)")
CB_SA=$(gcloud builds get-default-service-account --project=$PROJECT_ID \
  --format="value(serviceAccountEmail)" | sed 's|.*/||')
echo "Cloud Build SA: $CB_SA"

for ROLE in roles/run.admin roles/iam.serviceAccountUser \
            roles/artifactregistry.writer roles/logging.logWriter \
            roles/storage.objectViewer; do
  gcloud projects add-iam-policy-binding $PROJECT_ID \
    --member="serviceAccount:${CB_SA}" --role="$ROLE" --condition=None --quiet >/dev/null
done
```

> Cukup sekali per project. Gejala kalau ada yang kurang:
> - `run.admin` / `iam.serviceAccountUser` → step deploy gagal 403 / `iam.serviceaccounts.actAs`
> - `artifactregistry.writer` → step push gagal `denied: Permission "artifactregistry.repositories.uploadArtifacts"`
> - `logging.logWriter` → build gagal karena `logging: CLOUD_LOGGING_ONLY` tidak bisa menulis log
> - `storage.objectViewer` → build gagal membaca source yang di-upload `gcloud builds submit`

### cloudbuild.yaml

```yaml
# Jalankan manual:
#   gcloud builds submit --config=cloudbuild.yaml --substitutions=_TAG=v2 .
# $PROJECT_ID otomatis diisi Cloud Build sesuai project aktif — tidak perlu diedit.
steps:
  - name: 'gcr.io/cloud-builders/docker'
    args: ['build', '--platform', 'linux/amd64', '-t', '$_REGION-docker.pkg.dev/$PROJECT_ID/barista-bot/app:$_TAG', '.']

  - name: 'gcr.io/cloud-builders/docker'
    args: ['push', '$_REGION-docker.pkg.dev/$PROJECT_ID/barista-bot/app:$_TAG']

  - name: 'gcr.io/google.com/cloudsdktool/cloud-sdk'
    entrypoint: gcloud
    args:
      - run
      - deploy
      - barista-bot
      - --image=$_REGION-docker.pkg.dev/$PROJECT_ID/barista-bot/app:$_TAG
      - --region=$_REGION
      - --platform=managed

substitutions:
  _REGION: asia-southeast1
  _TAG: v2

options:
  logging: CLOUD_LOGGING_ONLY
```

### Submit build manual

```bash
# _TAG = tag image (default v2 di cloudbuild.yaml). Naikkan tiap submit:
gcloud builds submit --config=cloudbuild.yaml \
  --project=$PROJECT_ID \
  --substitutions=_TAG=v2 .

# Alternatif: versi beta (memakai _TAG default v2 dari cloudbuild.yaml)
gcloud beta builds submit --config=cloudbuild.yaml
```

> ⏱️ Build + push + deploy di Cloud Build memakan sekitar **3 menit**. Sambil menunggu, buka link log yang dicetak `gcloud builds submit` untuk melihat tiap step berjalan.
>
> Ganti `v2` dengan versi atau tag yang relevan setiap kali submit.
> `$PROJECT_ID` di `cloudbuild.yaml` adalah substitusi bawaan Cloud Build — otomatis terisi project aktif, tidak perlu diedit.

### Test setelah deploy via Cloud Build

Cloud Build tidak mengubah URL service — URL tetap sama seperti hasil Bagian 3. Yang berubah hanya **revision** (versi deployment) yang melayani traffic.

**1. Pastikan revision baru sudah aktif dan memakai image terbaru**

```bash
gcloud run services describe barista-bot --region=$REGION \
  --format="value(status.latestReadyRevisionName,spec.template.spec.containers[0].image)"
# contoh output:
# barista-bot-00002-xxx   asia-southeast1-docker.pkg.dev/<project-id>/barista-bot/app:v2
```

Nomor revision naik (`00001` → `00002`) dan tag image sesuai `_TAG` yang di-submit (`v2`).

**2. Ambil URL service**

```bash
SERVICE_URL=$(gcloud run services describe barista-bot \
  --region=$REGION --format="value(status.url)")
echo $SERVICE_URL
# contoh: https://barista-bot-xxxxxxxxxx-as.a.run.app
```

**3. Health check**

```bash
curl $SERVICE_URL/
# {"service":"barista-bot","status":"ok"}
```

**4. Tanya agent**

```bash
curl -X POST $SERVICE_URL/ask \
  -H "Content-Type: application/json" \
  -d '{"message": "Ada service yang bermasalah?"}'
```

Jawaban butuh sekitar 10–15 detik. Hasilnya berupa JSON `{"reply": "..."}` berisi ringkasan service yang bermasalah dan rekomendasinya.

**5. Lihat tool apa saja yang dipanggil agent**

```bash
gcloud run services logs read barista-bot --region=$REGION --limit=20 | grep "tool call"
# tool call: list_services map[]
# tool call: get_recent_logs map[service_name:user-service severity:ERROR]
```

Kalau ada minimal dua baris `tool call` untuk satu pertanyaan, agent sudah bekerja sebagai agent — bukan sekadar chatbot.

**Kalau gagal:**

| Gejala | Penyebab umum |
|---|---|
| `{"error":"generate: ... 403"}` / `API key not valid` | Isi secret `gemini-api-key` salah — tambah versi baru dengan `gcloud secrets versions add` |
| `{"error":"generate: ... 429"}` | Kena rate limit Gemini — tunggu sebentar lalu ulangi |
| `Service Unavailable` / container gagal start | Cek `gcloud run services logs read barista-bot --region=$REGION` |
| Revision tidak berubah | Build gagal — cek status di link log yang dicetak `gcloud builds submit` |

---

## Workshop Completion Checklist

Peserta dianggap berhasil kalau semua item ini terpenuhi di akhir sesi:

- [ ] Agent bisa menerima pertanyaan via HTTP POST `/ask`
- [ ] Agent memanggil minimal dua tools dalam satu request (contoh: `list_services` → `get_recent_logs`)
- [ ] Docker image berhasil dibangun dan dijalankan lokal
- [ ] Cloud Run memberikan URL HTTPS yang bisa diakses publik
- [ ] API key tidak ada di Dockerfile, kode, atau Git — disimpan di Secret Manager
- [ ] Pipeline Cloud Build berhasil build + deploy dari satu command

---

## Catatan Trainer — Antisipasi Masalah

Hal yang paling sering menghambat di workshop cloud:

1. **Waktu** — 50 menit tidak cukup untuk mengetik kode. Pastikan peserta clone starter repo; Bagian 1 adalah walkthrough. Kalau sesi molor, prioritaskan Bagian 3 (URL publik) dan jadikan Bagian 4 demo oleh trainer.
2. **GCP account peserta** — billing belum aktif (butuh kartu), atau akun organisasi kena org policy (`--allow-unauthenticated` ditolak, error `FAILED_PRECONDITION ... iam.allowedPolicyMemberDomains`). Kirim checklist pre-workshop H-3: pakai Gmail pribadi, buat project baru, aktifkan billing/redeem voucher, jalankan bagian **Setup** sampai selesai.
3. **IAM Cloud Build** — project baru memakai compute default SA tanpa role Editor. Pastikan peserta menjalankan loop `add-iam-policy-binding` di Bagian 4 sebelum `gcloud builds submit`.
4. **Gemini API key & rate limit** — wajibkan **satu key per peserta** (dibuat H-1 di AI Studio). Free tier punya batas request per menit per project, dan setiap `/ask` memanggil model 2–3 kali, jadi shared key untuk puluhan peserta akan cepat kena `429`. Mitigasi yang sudah ada di kode:
   - `generateWithRetry` di `agent.go` otomatis retry dengan backoff (2s → 4s → 8s) saat `429`/`503`.
   - Model bisa diganti tanpa ubah kode lewat env var `GEMINI_MODEL` (mis. varian "lite" yang kuotanya lebih longgar): `--set-env-vars=GEMINI_MODEL=<nama-model>` saat `gcloud run deploy`.
   - Shared key hanya untuk darurat, beberapa orang saja, dan **rotate/hapus key setelah workshop**.
5. **Platform mismatch** — Mac M-series build `linux/arm64` secara default. Ingatkan peserta `--platform linux/amd64` sejak awal. Kalau lupa, Cloud Run akan error `exec format error`. Peserta Cloud Shell tidak kena masalah ini.

---

## Hasil Akhir

```
✅  AI agent dengan Gemini function calling
✅  Container production-ready (Dockerfile multi-stage)
✅  Cloud Run service — publicly accessible URL
✅  Secret Manager untuk API key
✅  CI/CD pipeline via Cloud Build
```

---

## Cleanup

Hapus resource setelah workshop selesai untuk menghindari biaya yang tidak perlu.

```bash
# Hapus Cloud Run service
gcloud run services delete barista-bot \
  --region=$REGION \
  --project=$PROJECT_ID \
  --quiet

# Hapus Artifact Registry repo (beserta semua image di dalamnya)
gcloud artifacts repositories delete barista-bot \
  --location=$REGION \
  --project=$PROJECT_ID \
  --quiet

# # Hapus secret (opsional)
# gcloud secrets delete gemini-api-key \
#   --project=$PROJECT_ID \
#   --quiet
```

> Cloud Run scale to zero saat tidak ada traffic — biaya mendekati $0 kalau dibiarkan idle.
> Tapi Artifact Registry tetap dikenakan biaya storage per GB. Sebaiknya dihapus setelah workshop.

---

## Mode Real — Data GCP Sungguhan (Lokal)

Repo sudah menyertakan `tools_real.go`: implementasi keempat tools yang membaca **data sungguhan** dari project GCP. Aktifkan dengan env var `TOOLS_MODE=real` — tanpa env var ini, Barista Bot tetap memakai data simulasi.

| Tool | Mode simulasi (`tools.go`) | Mode real (`tools_real.go`) |
|---|---|---|
| `list_services` | map `services` | Cloud Run Admin API — daftar service di region |
| `check_service_health` | map `services` | Cloud Run Admin API — kondisi service & revision |
| `get_recent_logs` | map `mockLogs` | Cloud Logging API — log 1 jam terakhir |
| `get_deployment_status` | map `services` | Cloud Run Admin API — revision, image, traffic split |

Yang berubah hanya **sumber data**. `toolDeclarations` dan `agent.go` tetap sama — inilah keuntungan memisahkan deklarasi tool dari implementasinya. Satu-satunya perubahan di `tools.go` adalah percabangan di awal `executeTool`:

```go
func executeTool(ctx context.Context, name string, args map[string]any) map[string]any {
	if realMode() {                       // TOOLS_MODE=real
		return executeRealTool(ctx, name, args)
	}
	// ... data simulasi seperti sebelumnya
}
```

Contoh isi satu tool real — memanggil REST API Google dengan kredensial service account Cloud Run:

```go
func realCheckServiceHealth(ctx context.Context, name string) map[string]any {
	u, _ := runServiceURL(ctx, name) // https://run.googleapis.com/v2/projects/P/locations/R/services/NAME
	svc, err := callGCP(ctx, "GET", u, nil)
	if err != nil {
		return errResult(err)        // error dikembalikan ke Gemini, bukan bikin crash
	}
	cond := obj(svc, "terminalCondition")
	return map[string]any{
		"service":         name,
		"status":          serviceStatus(svc), // healthy / degraded / unhealthy
		"condition_state": str(cond, "state"),
		// ...
	}
}
```

### Menjalankan mode real di lokal

Jalankan Barista Bot di laptop memakai **akun gcloud sendiri**. Tidak perlu build image, deploy, atau membuat service account.

**1. Login kredensial untuk aplikasi (Application Default Credentials)**

```bash
gcloud auth application-default login
```

Perintah ini membuka browser. Login dengan akun Google yang punya akses ke project workshop. Kredensialnya disimpan di laptop dan otomatis dipakai oleh `tools_real.go`.

> ⚠️ Perintah ini **menimpa** ADC yang sudah ada di laptop. Kalau laptop juga dipakai untuk pekerjaan lain dengan akun berbeda, simpan ADC workshop di folder terpisah:
>
> ```bash
> CLOUDSDK_CONFIG=$HOME/.config/gcloud-workshop gcloud auth application-default login
> export GOOGLE_APPLICATION_CREDENTIALS=$HOME/.config/gcloud-workshop/application_default_credentials.json
> ```

**2. Jalankan dalam mode real**

```bash
export GEMINI_API_KEY=$(gcloud secrets versions access latest --secret=gemini-api-key)
TOOLS_MODE=real GOOGLE_CLOUD_PROJECT=$PROJECT_ID REGION=$REGION go run .
```

**3. Test dari terminal kedua**

```bash
curl -X POST http://localhost:8080/ask \
  -H "Content-Type: application/json" \
  -d '{"message": "Ada service Cloud Run yang bermasalah? Cek juga log error-nya."}'
```

Agent sekarang membaca service Cloud Run dan log sungguhan di project tersebut.

> 💡 Bagian ini dijalankan **setelah Cleanup**, jadi service `barista-bot` sudah terhapus. Agent akan melaporkan bahwa tidak ada service Cloud Run di region tersebut — itu jawaban yang benar, karena datanya sungguhan. Kalau project punya service Cloud Run lain, agent akan memeriksa service-service itu.

**Izin yang dibutuhkan** — akun yang dipakai cukup punya akses **baca**:

| Role | Untuk |
|---|---|
| `roles/run.viewer` | `list_services`, `check_service_health`, `get_deployment_status` |
| `roles/logging.viewer` | `get_recent_logs` |

Pemilik project (Owner/Editor) sudah otomatis punya keduanya. Kalau agent melaporkan `HTTP 403: PERMISSION_DENIED`, berarti akun di ADC tidak punya akses ke project tersebut — cek dengan `gcloud auth application-default print-access-token` dan pastikan login dengan akun yang benar.

> Mode real **hanya untuk dijalankan di lokal**. Jangan deploy mode ini ke Cloud Run: service yang terbuka ke internet akan memperlihatkan informasi infrastruktur sungguhan kepada siapa pun yang memanggilnya.

---

## Estimasi Biaya

Cloud Run sangat murah untuk demo:

| Resource | Free tier/bulan |
|---|---|
| Requests | 2 juta pertama gratis |
| CPU | 360.000 vCPU-detik gratis |
| Memory | 180.000 GiB-detik gratis |

> Untuk workshop ini: **praktis $0**.

---

## Setelah Workshop: Mengembangkan Barista Bot

Barista Bot hari ini sengaja dibuat sederhana: empat tools, data simulasi, semuanya hanya membaca. Pola yang sama dipakai untuk agent produksi — misalnya bot L1 Support di Slack yang menjawab pertanyaan developer:

```
@mention bot di Slack
    ↓
Agent (Gemini + system prompt, maksimal 8 putaran)
    ├── search_knowledge_base → cari dokumentasi internal
    ├── ask_clarification     → tanya balik kalau pertanyaan terlalu umum
    ├── escalate_to_human     → serahkan ke tim support kalau tidak ketemu jawaban
    └── close_ticket          → tandai selesai kalau user bilang beres
    ↓
Balas di thread Slack + link referensi dokumen
```

Strukturnya sama persis dengan Barista Bot: ada trigger, model, system prompt, daftar tools, dan loop yang dibatasi. Yang berbeda adalah **desain tools-nya**. Empat ide untuk mengembangkan Barista Bot:

1. **Pakai data sungguhan.** Ganti sumber data tools dengan Cloud Run Admin API dan Cloud Logging API. Deklarasi tool dan agentic loop tidak perlu berubah sama sekali — lihat bagian **Mode Real** setelah Cleanup.
2. **Tambah jalur ke manusia.** Agent yang baik tahu kapan menyerah. Tambahkan tool `escalate_to_oncall` yang mengirim ringkasan insiden ke channel on-call.
3. **Hati-hati dengan tool yang mengubah sesuatu.** Semua tool Barista Bot hanya membaca. Saat menambah tool seperti `restart_service` atau `rollback_deployment`, pikirkan dulu: **keputusan apa yang aman diserahkan ke AI?** Membaca log aman; restart atau rollback sebaiknya tetap butuh konfirmasi manusia.
4. **Sambungkan ke tempat tim bekerja.** Karena Barista Bot hanyalah endpoint `POST /ask`, ia bisa dipanggil dari Slack, cron job, atau pipeline CI tanpa mengubah kodenya.

---

### FAQ

**Kenapa datanya simulasi, bukan data sungguhan?**
Supaya semua peserta mendapat skenario yang sama (ada service *degraded* dan *unhealthy*) dan bisa langsung melihat agent menginvestigasi masalah. Project GCP baru biasanya belum punya service yang bermasalah untuk diperiksa.

**Kalau `tools.go` dihapus, Barista Bot jadi apa?**
Jadi chatbot biasa. Tanpa tools, Gemini tidak punya cara melihat kondisi sistem — ia hanya bisa menjawab umum atau, lebih buruk, mengarang status service. Tools-lah yang membuatnya menjadi *agent*.

**Apakah untuk pakai data sungguhan harus membuat RAG?**
Tidak. RAG cocok untuk pengetahuan yang **relatif tetap** (runbook, SOP, dokumentasi). Status service dan log berubah setiap saat dan tidak tersimpan di dokumen mana pun — untuk itu yang dibutuhkan adalah **API sungguhan**, seperti di `tools_real.go`.

RAG sendiri sebenarnya adalah **salah satu jenis tool** — tool untuk mencari dokumen. Keduanya paling kuat jika digabung:

1. `list_services` (API) → `user-service` degraded
2. `get_recent_logs` (API) → `connection refused (database)`
3. `search_runbook` (RAG) → langkah penanganan dari runbook tim
4. Jawaban: diagnosis **plus** langkah perbaikan sesuai SOP tim

**Apakah aman memberi agent akses ke sistem produksi?**
Aman selama izinnya dibatasi. Barista Bot mode real hanya memakai role *viewer* — ia bisa membaca, tidak bisa mengubah apa pun. Kalau nanti menambah tool yang mengubah sesuatu (restart, rollback), tambahkan langkah konfirmasi manusia sebelum aksi dijalankan.

---

## Referensi & Bacaan Lanjutan

### Untuk memahami konsep agent

| Judul | Link | Isi |
|---|---|---|
| **What are AI agents?** — Google | https://cloud.google.com/discover/what-are-ai-agents | Penjelasan Google tentang apa itu AI agent, komponen, dan use case |
| **Gemini API: Function Calling** | https://ai.google.dev/gemini-api/docs/function-calling | Cara kerja function calling di Gemini, lengkap dengan contoh kode Python |
| **Intro to Agents** — Hugging Face | https://huggingface.co/learn/agents-course/unit1/what-are-agents | Kursus gratis, unit 1 menjelaskan apa itu agent dan tools dengan sangat visual |
| **ReAct paper** (2022) | https://arxiv.org/abs/2210.03629 | Paper asli yang memperkenalkan pola Reasoning + Acting di LLM — dasar dari agentic loop |

### Untuk memahami Cloud Run & kontainerisasi

| Judul | Link | Isi |
|---|---|---|
| **Cloud Run: What is it?** | https://cloud.google.com/run/docs/overview/what-is-cloud-run | Penjelasan resmi Cloud Run — kapan pakai, cara kerja, limitasi |
| **Docker Get Started** | https://docs.docker.com/get-started/ | Tutorial resmi Docker dari nol, Part 1–3 cukup untuk workshop ini |
| **Dockerfile best practices** | https://docs.docker.com/develop/develop-images/dockerfile_best-practices/ | Termasuk kenapa urutan layer penting, cara minimize image size |
| **Containers explained** — GCP | https://cloud.google.com/learn/what-are-containers | Analogi yang bagus untuk memahami container vs VM |

### Untuk deployment & CI/CD

| Judul | Link | Isi |
|---|---|---|
| **Cloud Build quickstart** | https://cloud.google.com/build/docs/quickstart-deploy | Deploy ke Cloud Run via Cloud Build — persis yang kita lakukan di Bagian 4 |
| **Secret Manager best practices** | https://cloud.google.com/secret-manager/docs/best-practices | Kenapa tidak boleh taruh API key di env var atau Dockerfile |
| **Artifact Registry overview** | https://cloud.google.com/artifact-registry/docs/overview | Perbedaan Artifact Registry vs Container Registry (yang lama) |

### Urutan bacaan yang disarankan

Jika ini pertama kali menyentuh topik ini:

1. **Mulai dari containers**: baca Docker Get Started Part 1–3 (30 menit)
2. **Lanjut ke Cloud Run**: baca "What is Cloud Run" (10 menit)
3. **Pahami function calling**: baca Gemini Function Calling docs, jalankan contohnya (20 menit)
4. **Baca tentang agents**: artikel Google atau Hugging Face unit 1 (30 menit)
5. **Workshop ini** — semua potongan sudah masuk akal sekarang
