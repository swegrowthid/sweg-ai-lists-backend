# Rencana: Flow Upload Post + Taksonomi Category/Derivative

## Ringkasan

- Masalah: flow upload post baru butuh category bertingkat (coding -> claude code, amp) dan tiap blok post berupa .md yang isinya disimpan di database atau sebuah link.
- Pendekatan 1: adjacency list (categories.parent_id) dengan batas depth 2; tabel join post_categories tetap dipakai.
- Pendekatan 2: materialized path (kolom path + depth) dengan CHECK format sendiri; filter jadi prefix match tanpa self-join.
- Pendekatan 3: ltree (CREATE EXTENSION ltree, kolom ltree, index GiST); baca subtree tercepat dan kedalaman bebas, tapi butuh ekstensi.
- Rekomendasi: Pendekatan 1. Tiga pendekatan memakai aturan isi yang sama: .md disimpan di post_items.body_text, cap 64 KiB, transport JSON inline.

## Flow saat ini (dari kode)

Call graph produksi sekarang:

```text
POST /posts (Bearer)
  -> post.Handler.create (internal/post/http.go:126)
    -> decodeJSON, batas 1 MiB (internal/post/http.go:183-188)
      -> post.Service.Create (internal/post/service.go:88)
        -> normalizeCategorySlugs, 1..8 slug (internal/post/service.go:128)
        -> normalizeItems, 1..20 item (internal/post/service.go:150)
          -> post.Store.Create (internal/post/store_postgres.go:91)
            -> INSERT posts + post_categories + post_items, satu transaksi
GET /posts?category=<slug>&q=<teks>
  -> post.Handler.list (internal/post/http.go:97)
    -> post.Service.List (internal/post/service.go:65)
      -> post.Store.List (internal/post/store_postgres.go:120)
        -> EXISTS fc.slug = $1 (internal/post/store_postgres.go:128-131)
```

Fakta kode, tiap klaim dengan rujukan:

- Route sekarang: GET /categories, POST /categories (Bearer), GET /posts, POST /posts (Bearer), GET /posts/{slug} (internal/post/http.go:37-41).
- POST /posts terima JSON {slug, title, categories, items} (internal/post/http.go:49-54).
- Batas body JSON 1 MiB lewat http.MaxBytesReader (internal/post/http.go:184). Decoder memakai DisallowUnknownFields dan menolak lebih dari satu objek JSON (internal/post/http.go:185-192).
- Konstanta service: slug <= 100, title <= 200, name <= 100, category 1..8, item 1..20, body_text <= 64 KiB (internal/post/service.go:12-18; maxBodyTextLen di internal/post/service.go:17).
- Tabel categories masih flat: tanpa kolom parent (migrations/00003_create_categories.sql:2-8). slug UNIQUE + CHECK regex '^[a-z0-9]+(-[a-z0-9]+)*$' (migrations/00003_create_categories.sql:4). name CHECK 1..100 (migrations/00003_create_categories.sql:5). Trigger set_updated_at (migrations/00003_create_categories.sql:10-13).
- posts: FK author_id -> users (migrations/00004_create_posts.sql:8). post_categories: PK komposit (post_id, category_id) (migrations/00004_create_posts.sql:22-26) plus index idx_post_categories_category (migrations/00004_create_posts.sql:29).
- post_items: kind CHECK markdown|text|link|file (migrations/00004_create_posts.sql:34), body_text TEXT (migrations/00004_create_posts.sql:37), UNIQUE(post_id, position) (migrations/00004_create_posts.sql:49), CHECK post_items_kind_payload (migrations/00004_create_posts.sql:50-53).
- Kolom cdn_url dan cdn_provider sudah ada tetapi nullable dan belum dipakai (migrations/00004_create_posts.sql:44-47).
- Satu kind = satu bentuk payload. Field yang bukan milik kind ditolak walau kosong (internal/post/service.go:169-203). body_text menolak byte NUL (internal/post/service.go:205-219).
- Urutan tampil: ListCategories ORDER BY name, id (internal/post/store_postgres.go:62). Daftar post ORDER BY p.created_at DESC, p.id, c.name, c.slug (internal/post/store_postgres.go:133). Kategori dalam satu post ORDER BY c.name, c.slug (internal/post/store_postgres.go:225). Item ORDER BY position (internal/post/store_postgres.go:254).
- Filter `?category=<slug>` cocokkan satu slug persis lewat EXISTS tanpa hierarki (internal/post/store_postgres.go:128-131). Filter q memakai ILIKE dengan escapeLikePattern (internal/post/store_postgres.go:132, :317-321).
- Kontrak OpenAPI: CreatePostRequest wajib [slug, title, categories, items] (api/openapi.yaml:543). categories minItems 1, maxItems 8 (api/openapi.yaml:555-562). kind enum [markdown, text, link, file] (api/openapi.yaml:490, :575). body_text maksimal 64 KiB (api/openapi.yaml:495-497, :576-578). Parameter query category di GET /posts (api/openapi.yaml:153-156).
- Migrasi 00001-00004 murni DDL, tanpa pemindahan data. Deploy CI menjalankan goose up di server. Kami tidak mengklaim produksi sudah berisi data.

## Flow yang diminta

- User login lalu membuat post dengan token Bearer.
- User memilih satu category top: coding, design, video, dan seterusnya.
- Category boleh punya derivative, misalnya coding -> claude code dan coding -> amp.
- User boleh memilih satu derivative opsional dari turunan category itu.
- Tiap blok post berupa berkas .md. Isinya DISIMPAN DI DATABASE pada kolom post_items.body_text. Tanpa CDN dan tanpa object storage. Blok kedua berupa sebuah link.
- Aturan lama 1..8 category per post (internal/post/service.go:15) diganti: tepat satu category plus satu derivative opsional.

Alur pengguna:

```text
user (login)
  -> pilih category (coding)
    -> pilih derivative opsional (claude code)
      -> tambah blok .md (isinya ke body_text) atau blok link
        -> POST /posts (JSON inline)
```

Aturan isi yang sama untuk ketiga pendekatan:

- Isi .md disimpan di database pada post_items.body_text. Tanpa CDN. Tanpa object storage. cdn_url dan cdn_provider tetap NULL.
- Cap 64 KiB per body_text (maxBodyTextLen, internal/post/service.go:17). Alasan: TOAST menangani ukuran itu secara transparan; batas keras text sekitar 1 GB membuat 64 KiB keputusan aplikasi, bukan batas database; batas body 1 MiB di internal/post/http.go:184 adalah pembatas agregat nyata, dan 20 item x 64 KiB = 1,25 MiB membuat gabungan keduanya yang berlaku.
- Transport .md: JSON inline sebagai default. Satu kali decode, encoding/json memvalidasi UTF-8, DisallowUnknownFields berlaku untuk satu objek, penulisan atomik bersama seluruh post, dan service sudah menolak byte NUL. Varian multipart/form-data disebut di tiap pendekatan beserta konsekuensinya.

## Pendekatan 1 - Adjacency List Depth-2

Model taksonomi: hubungan induk-turunan disimpan pada kolom categories.parent_id. Batas depth 2. Tabel join post_categories tetap dipakai tanpa perubahan.

### Delta skema (goose Up/Down)

Berkas baru: migrations/00005_add_category_parent.sql. Semua perubahan skema hanya lewat migrasi baru ini. Migrasi 00001-00004 tidak boleh ditulis ulang. goose melacak versi di tabel goose_db_version, jadi menulis ulang 00001-00004 berisiko gagal atau konflik versi.

```sql
-- +goose Up
ALTER TABLE categories
  ADD COLUMN parent_id UUID REFERENCES categories(id) ON DELETE CASCADE;

CREATE INDEX idx_categories_parent_id ON categories (parent_id);

-- +goose StatementBegin
CREATE FUNCTION categories_depth_guard() RETURNS trigger AS $$
BEGIN
  IF NEW.parent_id IS NOT NULL THEN
    IF NEW.parent_id = NEW.id THEN
      RAISE EXCEPTION 'category cannot be its own parent';
    END IF;
    IF EXISTS (
      SELECT 1
      FROM categories parent
      WHERE parent.id = NEW.parent_id
        AND parent.parent_id IS NOT NULL
    ) THEN
      RAISE EXCEPTION 'category depth exceeds 2 levels';
    END IF;
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER categories_depth_guard
BEFORE INSERT OR UPDATE OF parent_id ON categories
FOR EACH ROW EXECUTE FUNCTION categories_depth_guard();
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS categories_depth_guard ON categories;
DROP FUNCTION IF EXISTS categories_depth_guard();
DROP INDEX IF EXISTS idx_categories_parent_id;
ALTER TABLE categories DROP COLUMN IF EXISTS parent_id;
```

Catatan skema:

- Batas depth 2 memakai trigger, bukan CHECK. CHECK di Postgres tidak boleh memuat subquery.
- Guard ini sekaligus mencegah cycle. Contoh A.parent = B dan B.parent = A ditolak karena salah satu pasti berdepth 2.
- Constraint yang dipertahankan tanpa perubahan: slug regex '^[a-z0-9]+(-[a-z0-9]+)*$' (migrations/00003_create_categories.sql:4), name 1..100 (migrations/00003_create_categories.sql:5), UNIQUE(post_id, position) (migrations/00004_create_posts.sql:49), FK author_id -> users (migrations/00004_create_posts.sql:8), CHECK post_items_kind_payload (migrations/00004_create_posts.sql:50-53), trigger set_updated_at (migrations/00003_create_categories.sql:10-13). Tidak ada constraint yang diubah.

### Delta API dan flow

- POST /posts menerima field `category` (slug induk, wajib) dan `derivative` (slug turunan, opsional). Field `categories` array dihapus; aturan 1..8 tidak berlaku lagi.
- POST /categories tetap seperti sekarang, plus field opsional `parent` (slug induk). Turunan dibuat sebagai category biasa dengan parent.
- (i) Jawaban: tepat satu kategori induk plus satu derivative opsional per post. Service menolak derivative yang bukan turunan langsung dari induk. post_categories tetap banyak-ke-banyak, jadi aturan "banyak kategori" bisa kembali tanpa skema baru; API membatasi 1+1.
- (ii) Jawaban: `?category=<slug>` IKUT mencocokkan turunan. Query EXISTS diganti satu self-join satu level: post cocok bila punya kategori fc.slug = $1 atau induk fc.slug = $1. Cukup satu self-join karena depth tetap 2.
- (iii) Jawaban: urutan tampil kategori berubah. GET /categories kini mengurutkan induk dulu lalu turunannya: ORDER BY COALESCE(parent.name, c.name), (c.parent_id IS NOT NULL), c.name, c.id. Kategori di dalam satu post juga induk dulu, lalu nama dan slug: ORDER BY (c.parent_id IS NOT NULL), c.name, c.slug. Jadi categories[0] selalu category yang dipilih dan categories[1] turunannya.
- Isi blok post: .md disimpan di database pada post_items.body_text, tanpa CDN dan tanpa object storage.
- Cap 64 KiB per body_text (maxBodyTextLen, internal/post/service.go:17), dengan alasan pada bagian "Flow yang diminta". Batas body 1 MiB di internal/post/http.go:184 tetap berlaku.
- Transport .md: JSON inline sebagai default. Satu decode lewat decodeJSON (internal/post/http.go:183-188), UTF-8 divalidasi encoding/json, NUL sudah ditolak service (internal/post/service.go:219). Varian multipart/form-data: tiap bagian perlu cek utf8.Valid manual, jaminan DisallowUnknownFields satu objek hilang, urutan bagian menentukan urutan blok, batas 1 MiB tetap berlaku lewat MaxBytesReader, dan CRLF tidak dinormalisasi (bukan korupsi).
- Response Post tetap memuat array `categories` berisi 1 atau 2 entri. Skema Category bertambah field `parent_slug` yang nullable.

Contoh request:

```json
{
  "slug": "setup-claude-code",
  "title": "Setup Claude Code",
  "category": "coding",
  "derivative": "claude-code",
  "items": [
    { "kind": "markdown", "body_text": "# Setup\n\nLangkah pertama ..." },
    { "kind": "link", "url": "https://docs.example.com/claude-code" }
  ]
}
```

### Biaya migrasi

- DDL: satu kolom nullable, satu index, satu function, satu trigger. ALTER TABLE ADD COLUMN nullable adalah perubahan metadata. CREATE INDEX memindai tabel categories; tabel ini kecil, jadi biayanya bisa diabaikan.
- Tanpa backfill. Semua baris lama otomatis jadi kategori induk (parent_id NULL).
- Kode berubah: struct Category, validasi service, query filter dan urutan di store_postgres serta store_memory, kontrak OpenAPI. Tabel posts dan post_items tidak berubah.
- Rollback: goose down mencabut trigger, function, index, dan kolom.
- Migrasi additif aman pada semua kasus data. Kami tidak mengklaim produksi sudah berisi data.

### Tradeoff

- Plus: perubahan paling kecil di antara tiga pendekatan. Rename slug O(1) tanpa rewrite turunan. Tanpa ekstensi. Batas dua level eksplisit di database.
- Minus: query turunan butuh self-join satu level. Bila kelak depth 3 dibutuhkan, trigger dan query harus diubah lagi. Guard depth hidup di trigger, bukan di CHECK murni.

### File yang tersentuh

- `migrations/00005_add_category_parent.sql` (baru)
- `internal/post/post.go` (struct Category tambah ParentSlug)
- `internal/post/service.go` (validasi category + derivative)
- `internal/post/store_postgres.go` (insert link kategori, filter turunan, urutan kategori)
- `internal/post/store_memory.go` (parity filter dan urutan)
- `internal/post/http.go` (bentuk request category + derivative)
- `api/openapi.yaml` (CreatePostRequest, Category)
- `internal/post/create_test.go`, `internal/post/list_test.go`, `internal/app/app_test.go`

## Pendekatan 2 - Materialized Path

Model taksonomi: categories menyimpan path (rangkaian slug dipisah titik, misalnya coding.claude-code) dan depth. Path punya CHECK format sendiri karena regex slug tidak menerima titik. Filter kategori jadi prefix match tanpa self-join.

### Delta skema (goose Up/Down)

Berkas baru: migrations/00005_add_category_path.sql. Semua perubahan skema hanya lewat migrasi baru ini. Migrasi 00001-00004 tidak boleh ditulis ulang.

```sql
-- +goose Up
ALTER TABLE categories
  ADD COLUMN path  TEXT,
  ADD COLUMN depth SMALLINT NOT NULL DEFAULT 0;

UPDATE categories SET path = slug, depth = 0 WHERE path IS NULL;

ALTER TABLE categories
  ALTER COLUMN path SET NOT NULL,
  ADD CONSTRAINT categories_path_format CHECK (
    path ~ '^[a-z0-9]+(-[a-z0-9]+)*([.][a-z0-9]+(-[a-z0-9]+)*)?$'
  ),
  ADD CONSTRAINT categories_path_depth CHECK (
    (depth = 0 AND path !~ '[.]') OR (depth = 1 AND path ~ '[.]')
  );

CREATE INDEX idx_categories_path ON categories (path);

-- +goose StatementBegin
CREATE FUNCTION categories_path_guard() RETURNS trigger AS $$
BEGIN
  IF NEW.depth = 1 AND NOT EXISTS (
    SELECT 1
    FROM categories parent
    WHERE parent.slug = split_part(NEW.path, '.', 1)
      AND parent.depth = 0
      AND parent.path = split_part(NEW.path, '.', 1)
  ) THEN
    RAISE EXCEPTION 'category parent not found for path %', NEW.path;
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER categories_path_guard
BEFORE INSERT OR UPDATE OF path ON categories
FOR EACH ROW EXECUTE FUNCTION categories_path_guard();
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS categories_path_guard ON categories;
DROP FUNCTION IF EXISTS categories_path_guard();
DROP INDEX IF EXISTS idx_categories_path;
ALTER TABLE categories
  DROP CONSTRAINT IF EXISTS categories_path_depth,
  DROP CONSTRAINT IF EXISTS categories_path_format,
  DROP COLUMN IF EXISTS depth,
  DROP COLUMN IF EXISTS path;
```

Catatan skema:

- CHECK categories_path_format adalah CHECK sendiri untuk format path. Regex slug (migrations/00003_create_categories.sql:4) tidak menerima titik, jadi path tidak bisa memakai regex yang sama.
- CHECK format sekaligus membatasi maksimal dua label. Kedalaman lebih dari dua ditolak oleh CHECK categories_path_depth.
- UPDATE backfill di atas aman bila tabel kosong maupun terisi. Kami tidak mengklaim produksi sudah berisi data.
- Constraint yang dipertahankan tanpa perubahan: slug regex (migrations/00003_create_categories.sql:4), name 1..100 (migrations/00003_create_categories.sql:5), UNIQUE(post_id, position) (migrations/00004_create_posts.sql:49), FK author_id -> users (migrations/00004_create_posts.sql:8), CHECK post_items_kind_payload (migrations/00004_create_posts.sql:50-53), trigger set_updated_at (migrations/00003_create_categories.sql:10-13). Yang ditambah hanya dua CHECK baru di categories.

### Delta API dan flow

- POST /posts menerima `category` (slug induk, wajib) dan `derivative` (slug turunan, opsional), sama seperti Pendekatan 1. Delta khusus pendekatan ini: skema Category di response bertambah `path` dan `depth`.
- POST /categories menerima field opsional `parent`. Service mengisi path dari path induk ditambah slug sendiri.
- (i) Jawaban: tepat satu kategori induk plus satu derivative opsional per post. CHECK categories_path_format membatasi dua label, jadi model tidak menyimpan rantai lebih panjang. "Banyak kategori per post" berarti banyak baris post_categories; API tetap membatasi 1+1.
- (ii) Jawaban: `?category=<slug>` IKUT mencocokkan turunan, lewat prefix match tanpa self-join: WHERE c.path = $1 OR c.path LIKE $1 || '.%'. Slug tidak memuat '%', '_', atau '.', jadi pola LIKE selalu literal.
- (iii) Jawaban: urutan tampil memakai ORDER BY path. Sifatnya pre-order: induk selalu sebelum turunan ('coding' sebelum 'coding.amp'), sibling mengikuti urutan leksikografis slug. Kategori dalam satu post tetap ORDER BY c.name, c.slug (internal/post/store_postgres.go:225).
- Isi blok post: .md disimpan di database pada post_items.body_text, tanpa CDN dan tanpa object storage.
- Cap 64 KiB per body_text (maxBodyTextLen, internal/post/service.go:17), dengan alasan pada bagian "Flow yang diminta". Batas body 1 MiB di internal/post/http.go:184 tetap berlaku.
- Transport .md: JSON inline sebagai default. Satu decode, UTF-8 divalidasi encoding/json, NUL sudah ditolak service (internal/post/service.go:219). Varian multipart/form-data: perlu utf8.Valid manual per bagian, jaminan DisallowUnknownFields satu objek hilang, batas 1 MiB tetap berlaku, dan CRLF tidak dinormalisasi (bukan korupsi).

### Biaya migrasi

- DDL: dua kolom, dua CHECK, satu index btree, satu function, satu trigger, satu UPDATE backfill.
- UPDATE backfill menyentuh semua baris categories. Pada dua level biayanya kecil. Migrasi tetap additif; kami tidak mengklaim produksi sudah berisi data.
- Rename slug induk mengubah path semua turunan (O(n) baris). Pada jumlah turunan kecil biaya ini bisa diabaikan.
- Kode berubah seperti Pendekatan 1, ditambah pengisi path di store dan perhitungan ulang saat rename.

### Tradeoff

- Plus: filter turunan jadi range scan btree tanpa self-join. Urutan tree gratis dari ORDER BY path. Tanpa ekstensi.
- Minus: path menduplikasi slug. Rename induk menulis ulang turunan. Integritas hubungan induk dijaga trigger dan service, bukan FK. Format path butuh CHECK sendiri karena regex slug tidak menerima titik.

### File yang tersentuh

- `migrations/00005_add_category_path.sql` (baru)
- `internal/post/post.go` (struct Category tambah Path dan Depth)
- `internal/post/service.go` (validasi category + derivative, turunan path)
- `internal/post/store_postgres.go` (isi path, filter prefix, ORDER BY path)
- `internal/post/store_memory.go` (parity filter dan urutan)
- `internal/post/http.go` (bentuk request category + derivative)
- `api/openapi.yaml` (CreatePostRequest, Category bertambah path dan depth)
- `internal/post/create_test.go`, `internal/post/list_test.go`, `internal/app/app_test.go`

## Pendekatan 3 - ltree

Model taksonomi: categories menyimpan tree_path bertipe ltree dari ekstensi ltree. Index GiST menopang operator <@. Kedalaman lebih dari dua terbuka.

### Delta skema (goose Up/Down)

Berkas baru: migrations/00005_add_category_ltree.sql. Semua perubahan skema hanya lewat migrasi baru ini. Migrasi 00001-00004 tidak boleh ditulis ulang.

```sql
-- +goose Up
CREATE EXTENSION IF NOT EXISTS ltree;

ALTER TABLE categories
  ADD COLUMN tree_path ltree;

UPDATE categories
SET tree_path = replace(slug, '-', '_')::ltree
WHERE tree_path IS NULL;

ALTER TABLE categories ALTER COLUMN tree_path SET NOT NULL;

CREATE INDEX idx_categories_tree_path_gist ON categories USING GIST (tree_path);

-- +goose StatementBegin
CREATE FUNCTION categories_tree_guard() RETURNS trigger AS $$
BEGIN
  IF nlevel(NEW.tree_path) > 1 AND NOT EXISTS (
    SELECT 1
    FROM categories parent
    WHERE parent.tree_path = subpath(NEW.tree_path, 0, nlevel(NEW.tree_path) - 1)
  ) THEN
    RAISE EXCEPTION 'category parent not found for tree_path %', NEW.tree_path;
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER categories_tree_guard
BEFORE INSERT OR UPDATE OF tree_path ON categories
FOR EACH ROW EXECUTE FUNCTION categories_tree_guard();
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS categories_tree_guard ON categories;
DROP FUNCTION IF EXISTS categories_tree_guard();
DROP INDEX IF EXISTS idx_categories_tree_path_gist;
ALTER TABLE categories DROP COLUMN IF EXISTS tree_path;
DROP EXTENSION IF EXISTS ltree;
```

Catatan skema:

- Label ltree memakai replace(slug, '-', '_'). Dokumen PostgreSQL sekarang mengizinkan huruf, angka, underscore, dan hyphen pada label, tergantung locale database. Versi yang lebih lama hanya mengizinkan [A-Za-z0-9_]. Transformasi ini selalu aman dan injektif karena regex slug (migrations/00003_create_categories.sql:4) menolak underscore. Verifikasi versi Postgres target sebelum implementasi.
- Kedalaman lebih dari dua terbuka. Path coding.ai.claude_code sah tanpa perubahan skema.
- Constraint yang dipertahankan tanpa perubahan: slug regex (migrations/00003_create_categories.sql:4), name 1..100 (migrations/00003_create_categories.sql:5), UNIQUE(post_id, position) (migrations/00004_create_posts.sql:49), FK author_id -> users (migrations/00004_create_posts.sql:8), CHECK post_items_kind_payload (migrations/00004_create_posts.sql:50-53), trigger set_updated_at (migrations/00003_create_categories.sql:10-13). Tidak ada constraint yang diubah.

### Delta API dan flow

- POST /posts menerima `category` (slug induk, wajib) dan `derivative` (slug turunan, opsional). Delta khusus pendekatan ini: skema Category di response bertambah `tree_path` (label ltree) dan `depth` (hasil nlevel).
- POST /categories menerima field opsional `parent` pada level berapa pun, misalnya coding -> ai -> claude-code.
- (i) Jawaban: satu rantai kategori per post, bukan banyak kategori independen. Flow default tetap tepat satu induk plus satu derivative opsional. Skema tidak membatasi depth, jadi derivative boleh berada di bawah rantai yang lebih panjang.
- (ii) Jawaban: `?category=<slug>` IKUT mencocokkan semua turunan berapa pun depth, lewat WHERE c.tree_path <@ anchor. Operator <@ didukung index GiST.
- (iii) Jawaban: urutan tampil memakai ORDER BY tree_path. Perbandingan ltree mengikuti traversal tree dengan anak urut label, jadi induk selalu sebelum turunan. Kategori dalam satu post tetap ORDER BY c.name, c.slug (internal/post/store_postgres.go:225). Catatan: label memakai hasil transformasi slug, jadi urutan sibling sedikit berbeda dari urutan slug asli.
- Isi blok post: .md disimpan di database pada post_items.body_text, tanpa CDN dan tanpa object storage.
- Cap 64 KiB per body_text (maxBodyTextLen, internal/post/service.go:17), dengan alasan pada bagian "Flow yang diminta". Batas body 1 MiB di internal/post/http.go:184 tetap berlaku.
- Transport .md: JSON inline sebagai default. Satu decode, UTF-8 divalidasi encoding/json, NUL sudah ditolak service (internal/post/service.go:219). Varian multipart/form-data: perlu utf8.Valid manual per bagian, jaminan DisallowUnknownFields satu objek hilang, batas 1 MiB tetap berlaku, dan CRLF tidak dinormalisasi (bukan korupsi).

### Biaya migrasi

- DDL: CREATE EXTENSION ltree, satu kolom ltree, satu UPDATE backfill, satu index GiST, satu function, satu trigger.
- Ekstensi ltree tergolong trusted, jadi bisa dipasang tanpa superuser selama user punya CREATE pada database. Ekstensi tetap harus tersedia di semua environment: lokal, CI, dan server deploy. Repo sudah memakai ekstensi pg_trgm (migrations/00004_create_posts.sql:2), jadi ini dependensi kedua, bukan yang pertama.
- Backfill menyentuh semua baris categories. Index GiST memindai tabel saat dibuat. Kami tidak mengklaim produksi sudah berisi data.
- Kode berubah seperti Pendekatan 2, ditambah konversi slug ke label ltree di store.

### Tradeoff

- Plus: baca subtree tercepat lewat <@ dengan index GiST. Kedalaman bebas. Operator lquery dan ltxtquery tersedia untuk pencarian maju.
- Minus: dependensi ekstensi di semua environment. Grammar label ltree bergantung versi dan locale, jadi slug perlu transformasi. Rename induk tetap menulis ulang turunan (O(n)). Tim harus familiar dengan operator ltree.

### File yang tersentuh

- `migrations/00005_add_category_ltree.sql` (baru)
- `internal/post/post.go` (struct Category tambah TreePath dan Depth)
- `internal/post/service.go` (validasi category + derivative, konversi label)
- `internal/post/store_postgres.go` (kueri <@, konversi slug ke label)
- `internal/post/store_memory.go` (parity filter dan urutan, emulasi <@)
- `internal/post/http.go` (bentuk request category + derivative)
- `api/openapi.yaml` (CreatePostRequest, Category bertambah tree_path dan depth)
- `internal/post/create_test.go`, `internal/post/list_test.go`, `internal/app/app_test.go`

## Rekomendasi (tepat satu)

Pendekatan 1 - Adjacency List Depth-2: perubahan paling kecil dengan batas dua level yang eksplisit, cukup untuk flow yang diminta, tanpa dependensi ekstensi dan tanpa duplikasi path.

Keadaan akhir yang dituju: user login memilih satu category induk dan satu derivative opsional, tiap blok .md tersimpan di post_items.body_text tanpa CDN, filter `?category=<slug>` mencakup turunan, dan semuanya tiba lewat migrasi additif migrations/00005 tanpa menulis ulang migrasi 00001-00004.

## Keputusan yang masih terbuka

Rekomendasi sudah menetapkan default pada tiap sumbu di bawah. User boleh menimpa.

- Jumlah kategori per post: default tepat satu category induk plus satu derivative opsional. Aturan lama 1..8 (internal/post/service.go:15) dihapus. Boleh ditimpa jadi banyak kategori per post.
- Wajib atau tidaknya derivative: default opsional. Post tanpa derivative sah. Boleh ditimpa jadi wajib.
- Set kind: sekarang markdown, text, link, file (migrations/00004_create_posts.sql:34). Default rekomendasi: persempit jadi markdown dan link. Boleh ditimpa mempertahankan empat kind.
- Transport .md: default JSON inline. Boleh ditimpa jadi multipart/form-data dengan konsekuensi yang disebut di tiap pendekatan.

## Verifikasi

Skenario untuk implementasi nanti. Tiap skenario punya perintah dan satu penentu PASS atau FAIL.

- Migrasi naik turun naik di database kosong: `goose -dir migrations postgres "$DB_URL" up` lalu `down` lalu `up`. PASS bila ketiganya exit code 0.
- `go vet ./...` bersih dan `go test ./...` lulus, termasuk create_test.go, list_test.go, dan app_test.go.
- Buat post dengan category induk dan derivative: `curl -i -X POST localhost:8080/posts -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" -d '{"slug":"setup-claude-code","title":"Setup","category":"coding","derivative":"claude-code","items":[{"kind":"markdown","body_text":"# Setup"}]}'`. PASS bila status 201.
- Filter induk mencakup turunan: `curl -i "localhost:8080/posts?category=coding"`. PASS bila body memuat post coding dan post turunannya.
- Derivative yang bukan turunan induk ditolak: request yang sama dengan `"category":"design","derivative":"claude-code"`. PASS bila status 400.
- Cap body_text: payload dengan body_text 65537 byte. PASS bila status 400. Payload 65536 byte. PASS bila status 201.
- Byte NUL pada body_text ditolak. PASS bila status 400.
- Urutan GET /categories: induk lebih dulu lalu turunannya. PASS bila urutan body sesuai.
- Jenis transport JSON inline tetap satu objek JSON. Request dengan dua objek JSON ditolak 400.
- Cek kontrak: api/openapi.yaml sesuai bentuk request dan response baru. PASS bila validasi OpenAPI tanpa error.

## Sumber

- `internal/post/http.go:33-42, :97-110, :126-164, :183-194`
- `internal/post/service.go:12-18, :65-86, :88-118, :128-147, :150-219`
- `internal/post/store_postgres.go:62, :91-115, :120-146, :174-192, :194-215, :217-284, :317-319`
- `migrations/00003_create_categories.sql:2-18`
- `migrations/00004_create_posts.sql:2-60`
- `api/openapi.yaml:92, :153-156, :483-580`
- Laporan harvest lane 2026-09-27: `.omo/senpi-task/dag/results/dag_7e6ebc13-96c3-4bb5-abbe-052b3ce220e2/<lane>.txt` (enam lane paralel)
- PostgreSQL ltree: https://www.postgresql.org/docs/current/ltree.html
- Catatan migrasi: goose melacak versi di tabel goose_db_version. Migrasi 00001-00004 tidak boleh ditulis ulang. Semua perubahan skema lewat migrasi additif baru 00005. Kami tidak mengklaim produksi sudah berisi data.
