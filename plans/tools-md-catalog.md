# Rencana API Katalog Tools (sumber: markdown repo tools-ai-swe-growth)

## Context

- User minta API untuk menyajikan data dari repo `github.com/swegrowthid/tools-ai-swe-growth`.
- Repo itu menyimpan 3 file markdown sebagai database: `providers.md` (156 baris),
  `codingagents.md` (54), `ade.md` (49).
- User minta tanpa tabel database; alternatif DB diizinkan bila lebih optimal.
- Keputusan: file .md tetap jadi database. Tidak ada tabel SQL untuk katalog.
- Alasan: data read-only, sudah diverifikasi upstream di git; tabel hanya
  menduplikasi sumber kebenaran tanpa menambah kemampuan. Pola `internal/news`
  (fetch sumber + snapshot + scheduler harian) dipakai ulang tanpa adapter Postgres.

## Approach

- Paket `internal/tools` mengikuti pola 4 peran repo.
- `tools.go`: bentuk `Tool` (field bersama + field per kategori) dan `Category`
  (mapping statis slug/name/prefix/file + count live). Error bertag:
  `ErrNotFound`, `ErrUnknownCategory`, `ErrInvalidInput`.
- `parser.go`: cari tabel pertama yang headernya memuat kolom `ID` (legend
  `Symbol | Arti` di providers.md ikut ter-skip), petakan kolom lewat nama
  header, satu baris rusak di-skip, file tanpa tabel ID = error.
- `service.go`: `Sync` fetch ketiga file berurutan, parse, lalu
  `store.ReplaceAll` atomik; satu gagal = snapshot lama tetap. `List` validasi
  filter (NUL -> `ErrInvalidInput`, kategori asing -> `ErrUnknownCategory`),
  `category` terima slug atau prefix case-insensitive. `Get` normalisasi id
  ke uppercase. `Categories` = mapping + count dari snapshot.
- `store_memory.go`: snapshot `[]Tool` + index id di balik `RWMutex`.
- `scheduler.go`: sekali saat start, lalu tiap 00:00 waktu server (pola news).
- `http.go`: `GET /tools` (`?category=`, `?q=`), `GET /tools/categories`,
  `GET /tools/{id}`; semua publik, tanpa endpoint tulis.

Call graph:

```text
cmd/api / cmd/memserver
  -> go app.RunToolsSync(ctx)
    -> tools.Scheduler.Run
      -> tools.Service.Sync
        -> fetch providers.md + codingagents.md + ade.md (raw.githubusercontent.com)
        -> parseTools (tabel markdown -> []Tool)
        -> tools.Store.ReplaceAll (memory snapshot)

mux
  -> tools.Handler.list       -> Service.List -> Store.List
  -> tools.Handler.categories -> Service.Categories
  -> tools.Handler.get        -> Service.Get -> Store.Get
```

## Bentuk kontrak

- `Tool`: `id`, `category`, `name`, `website`, `website_label`, `status`
  (lowercase, emoji dibuang), `updated` (`YYYY-MM-DD`, mentah bila tak dikenal).
- Providers saja: `top_up`/`subscribe` (*bool, `false` tetap tampil) dan
  `min_spend` (teks mentah). ADE saja: `ai_features` ([]string). Field milik
  kategori lain absen dari JSON (`omitempty` + pointer).
- `Category`: `slug`, `name`, `prefix`, `source_file`, `source_url`, `count`.
- Mapping kategori statis di `categories` registry (mirror README sumber):
  `providers`/P/`providers.md`, `coding-agents`/CA/`codingagents.md`,
  `ade`/ADE/`ade.md`.

## Files

- `internal/tools/{tools,parser,service,store_memory,scheduler,http}.go` baru.
- `internal/tools/testdata/{providers,codingagents,ade}.md` potongan asli.
- `internal/app/app.go`, `cmd/api/main.go`, `cmd/memserver/main.go` wiring.
- `internal/app/app_test.go` route test. `api/openapi.yaml`, `README.md` kontrak.

## Verification

- `go vet ./...`, `gofmt`, `go test ./...` hijau; `-race` pada tools + app.
- E2E `cmd/memserver` ke GitHub asli: categories count 156/54/49 (persis
  total di footer sumber), filter slug+prefix+q, detail case-insensitive,
  400 `unknown category`, 404 `tool not found`.
