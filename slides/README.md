# Slide — Barista Bot Ops (DevFest Jogjakarta 2026)

Deck HTML mandiri, konten dari `../gdg-workshop-barista-bot.md`, desain mengikuti template DevFest Jogjakarta 2026.

## Menjalankan

```bash
cd slides
python3 -m http.server 8000
# buka http://localhost:8000
```

Bisa juga dibuka langsung: `open index.html`. Tidak butuh internet. Font memakai Google Sans kalau terpasang; jika tidak, otomatis memakai font sistem.

## Kontrol

| Tombol | Fungsi |
|---|---|
| → / Space / PageDown | slide berikutnya |
| ← / PageUp | slide sebelumnya |
| Home / End | slide pertama / terakhir |
| N | catatan pembicara |
| O | daftar semua slide |
| F | layar penuh |
| Esc | tutup catatan / daftar |

URL `index.html#7` langsung membuka slide 7.

## Slide interaktif

- **8 · Agentic loop** — tombol "Animasikan loop" menyorot langkah 01→02→03→…→04.
- **12 · Demo** — memutar ulang output asli dari test run (log `tool call` + jawaban agent). Tidak memanggil API, aman kalau Wi-Fi venue bermasalah.
- **16 · Checklist** — bisa dicentang saat sesi.

## Edit

Setiap slide adalah satu `<section class="slide">` di `index.html`. Catatan pembicara ada di `<div class="notes-src">` di dalam slide yang sama. Profil pembicara ada di slide 2 (foto: `assets/profile.jpg`). Slide 2 juga berisi perintah `git clone` dengan tombol Salin — paling andal saat dibuka lewat http://localhost.
