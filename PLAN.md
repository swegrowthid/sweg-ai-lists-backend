# Rencana API Register User

## Context

- Tambahkan endpoint register untuk tabel `users`.
- Tabel berada di `migrations/00001_create_users.sql`.
- Tabel memakai `username`, `email`, `password_hash`, UUID, dan timestamp.
- Password tidak boleh tersimpan sebagai plain text.
- Kode memakai Go stdlib, `database/sql`, dan port `user.Store`.
- Riset best practice Go akan memakai Exa.

## Approach

- Tambah use case register pada `internal/user`.
- Validasi request pada batas HTTP.
- Gunakan `golang.org/x/crypto/bcrypt` dengan cost minimum 10.
- Tolak password lebih dari 72 byte karena batas bcrypt di Go.
- Hash password sebelum data masuk ke store.
- Simpan hanya `password_hash` melalui query parameterized.
- Mapping error duplicate `username` atau `email` ke respons HTTP yang aman.
- Gunakan pesan dan status duplicate yang tidak membedakan apakah identifier sudah terdaftar.
- Gunakan `errors.As` ke `github.com/jackc/pgx/v5/pgconn.PgError` dan SQLSTATE `23505` pada adapter PostgreSQL.
- Batasi body request, tolak field JSON yang tidak dikenal, dan pastikan body berisi satu nilai JSON.
- Tambah route `POST /users/register`.
- Update OpenAPI agar kontrak request dan response sesuai implementasi.
- Tambah test untuk hash, validasi, duplicate, normalisasi email, dan handler.

Riset Exa:

- OWASP merekomendasikan password hashing adaptif seperti Argon2id atau bcrypt.
- `bcrypt.GenerateFromPassword` membuat salt sendiri dan menyediakan `CompareHashAndPassword`.
- Dokumentasi Go menyatakan bcrypt tidak menerima password lebih dari 72 byte.
- `encoding/json.Decoder.DisallowUnknownFields` menolak field yang tidak dikenal.
- `http.MaxBytesReader` membatasi ukuran body untuk mencegah penggunaan resource berlebihan.
- Dokumentasi pgx menyarankan `errors.As` dan `*pgconn.PgError` untuk error PostgreSQL.

Sumber:

- https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html
- https://pkg.go.dev/golang.org/x/crypto/bcrypt
- https://pkg.go.dev/encoding/json
- https://pkg.go.dev/net/http
- https://github.com/jackc/pgx/wiki/Error-Handling

Call graph:

```text
POST /users/register
  -> user.Handler.register
    -> decode and validate RegisterRequest
      -> user.Service.Register
        -> passwordHasher.Hash
        -> user.Store.Create
          -> user.PostgresStore.Create
            -> INSERT users
```

## Files to modify

- `internal/user/user.go` - bentuk input register, user, dan error domain.
- `internal/user/service.go` - use case register dan hashing.
- `internal/user/store_postgres.go` - insert user dan translate error database.
- `internal/user/store_memory.go` - dukung register untuk test dan mode tanpa DB.
- `internal/user/http.go` - route, decode, validasi, status code, dan response.
- `internal/user/*_test.go` - test unit/integration sesuai pola repo.
- `api/openapi.yaml` - kontrak endpoint register.
- `go.mod` dan `go.sum` - tambah `golang.org/x/crypto` jika belum tersedia.

## Reuse

- `user.Store` tetap menjadi port persistence.
- `user.Service` tetap menjadi pemilik use case.
- `user.Handler` tetap menjadi pemilik status HTTP.
- `User.PasswordHash` tetap memakai `json:"-"`.
- Migration tetap menjadi sumber bentuk kolom dan constraint.
- `internal/platform/db` tetap memasok `*sql.DB`.

## Steps

- [x] Riset Exa tentang password hashing, request validation, duplicate handling, dan Go HTTP API.
- [x] Pilih bcrypt dengan cost minimum 10 dan batas maksimum 72 byte.
- [x] Tetapkan endpoint `POST /users/register`.
- [x] Tetapkan response sukses berupa user tanpa password atau password hash.
- [x] Tetapkan register tidak menghasilkan token.
- [x] Tetapkan email dipangkas dan diubah menjadi lowercase.
- [x] Tetapkan duplicate memakai pesan generik tanpa menyebut field yang bentrok.
- [x] Implementasikan register pada service, store, memory store, dan handler.
- [x] Selaraskan OpenAPI.
- [x] Tambah test yang memverifikasi password tersimpan sebagai hash dan tidak muncul di response.

## Verification

- `go test ./...`
- `go vet ./...`
- Test endpoint dengan request valid.
- Test username dan email duplicate.
- Test password invalid atau terlalu panjang.
- Test response tidak memuat `password` atau `password_hash`.
- Jika database tersedia, test insert ke PostgreSQL sesuai constraint migration.

## Open questions

Semua keputusan sudah ditetapkan:

- Endpoint: `POST /users/register`.
- Response sukses: user tanpa password.
- Token: tidak dibuat saat register.
- Email: lowercase.
- Duplicate: pesan generik tanpa menyebut field yang bentrok.
