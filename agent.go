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
