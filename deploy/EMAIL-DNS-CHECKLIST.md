# Checklist DNS email OTP

Dokumen ini menutup satu masalah: email OTP `swegai sign-in code` masuk folder
spam di Gmail. Penyebabnya bukan kode aplikasi. Penyebabnya DNS domain
pengirim belum terautentikasi.

Kerjakan berurutan. Jangan lompat ke langkah 6 sebelum langkah 1 sampai 5
selesai.

## Kondisi awal (diukur 2026-10-31)

Hasil query langsung ke nameserver otoritatif Cloudflare:

| Record | Nama | Hasil |
| --- | --- | --- |
| SPF | `ai-sweg.my.id` | tidak ada TXT sama sekali |
| DKIM | `*._domainkey.ai-sweg.my.id` | tidak ada di selector mana pun |
| DMARC | `_dmarc.ai-sweg.my.id` | tidak ada |
| MX | `ai-sweg.my.id` | tidak ada |
| PTR | `43.165.198.116` | tidak ada |
| Umur domain | - | dibuat 2026-09-16 |

`my.id` ada di Public Suffix List. Jadi `_dmarc.my.id` tidak berlaku untuk
`ai-sweg.my.id`. Domain ini butuh record DMARC sendiri.

`43.165.198.116` adalah relay bersama milik Aceville Pte Ltd. Beberapa penerima
menolak atau menurunkan skor email dari IP tanpa PTR. Ini keterbatasan pihak
ketiga. Lihat bagian Risiko sisa.

## Langkah 1 - Buka panel email Sumopod

1. Masuk ke `https://sumopod.com/login`.
2. Buka menu layanan email atau SMTP.
3. Cari domain `ai-sweg.my.id`.
4. Jika domain belum terdaftar, tambahkan domain itu lebih dulu.
5. Setelah domain muncul, buka detail domain. Panel akan menampilkan record
   DNS yang harus dipasang.

Catat tiga nilai dari panel. Nilai ini tidak bisa ditebak dan tidak ada di repo:

- nilai `include` untuk SPF, misalnya `spf.<sesuatu>.com`
- nama selector DKIM, misalnya `<selector>`
- isi record DKIM, bisa TXT atau CNAME

Jika panel hanya menampilkan CNAME DKIM, ikuti CNAME. Jangan diubah jadi TXT.

## Langkah 2 - Pasang SPF

Di Cloudflare, domain `ai-sweg.my.id`:

1. Buka `DNS` lalu `Records`.
2. Klik `Add record`.
3. Isi:
   - Type: `TXT`
   - Name: `@`
   - Content: `v=spf1 include:<nilai-dari-panel> ~all`
   - TTL: `Auto`
4. Simpan.

Syarat: satu domain hanya boleh punya satu record SPF. Jika sudah ada TXT
`v=spf1`, gabungkan `include` ke record lama, jangan buat record kedua.

## Langkah 3 - Pasang DKIM

Di panel Sumopod, salin record DKIM apa adanya.

Di Cloudflare:

1. Klik `Add record`.
2. Type: `TXT` atau `CNAME`, sesuai panel.
3. Name: `<selector>._domainkey` tanpa domain di belakangnya.
4. Content: tempel persis dari panel. Jangan tambah spasi atau tanda kutip.
5. TTL: `Auto`.
6. Simpan.

Catatan Cloudflare: untuk TXT panjang, Cloudflare memecah otomatis. Tempel satu
baris penuh. Jangan pecah manual.

## Langkah 4 - Pasang DMARC

Ganti `KOTAK-YANG-KAMU-BACA@example.com` dengan alamat nyata yang kamu pantau.
Jangan pakai alamat `@ai-sweg.my.id` selama domain ini Null MX, karena laporan
akan memantul.

1. Klik `Add record`.
2. Type: `TXT`.
3. Name: `_dmarc`.
4. Content:

   ```
   v=DMARC1; p=none; rua=mailto:KOTAK-YANG-KAMU-BACA@example.com; adkim=s; aspf=s; fo=1
   ```

5. TTL: `Auto`.
6. Simpan.

Mulai dari `p=none`. Setelah laporan bersih minimal tujuh hari, ubah bertahap:

```
p=none  ->  p=quarantine  ->  p=reject
```

Jangan langsung `p=reject`. Email sah bisa ikut terbuang saat record belum
lengkap.

## Langkah 5 - Pasang Null MX

Hanya jika domain ini tidak menerima email masuk. Semua balasan ke `noreply@`
akan memantul, dan itu memang yang diinginkan.

1. Klik `Add record`.
2. Type: `MX`.
3. Name: `@`.
4. Mail server: `.` (satu titik).
5. Priority: `0`.
6. Simpan.

Lewati langkah ini jika domain ini juga dipakai menerima email.

## Langkah 6 - Verifikasi dari terminal

Tunggu propagasi. Cloudflare biasanya di bawah lima menit.

```sh
NS=hunts.ns.cloudflare.com

# SPF: harus keluar satu baris berisi v=spf1 dan include dari panel
dig @$NS +short TXT ai-sweg.my.id

# DKIM: harus keluar kunci, atau CNAME yang menunjuk ke panel
dig @$NS +short TXT <selector>._domainkey.ai-sweg.my.id
dig @$NS +short CNAME <selector>._domainkey.ai-sweg.my.id

# DMARC: harus keluar v=DMARC1
dig @$NS +short TXT _dmarc.ai-sweg.my.id

# Null MX: harus keluar "0 ."
dig @$NS +short MX ai-sweg.my.id
```

Jika salah satu kosong, record belum tersimpan atau namanya salah. Perbaiki
sebelum lanjut.

## Langkah 7 - Uji kirim dan baca hasil autentikasi

```sh
cd <repo>
make mailtest
```

Lalu di Gmail:

1. Buka email yang masuk. Cek folder Inbox dan Spam.
2. Klik titik tiga, pilih `Show original`.
3. Cari blok `Authentication-Results`. Target hasil:

   ```
   spf=pass
   dkim=pass
   dmarc=pass
   ```

Jika `dkim=none`, berarti Sumopod tidak menandatangani pesan. Record DKIM saja
tidak cukup. Hubungi dukungan Sumopod dan minta signing aktif untuk domain ini.

Jika `spf=pass` tetapi tetap masuk spam, penyebabnya reputasi IP relay. Lihat
bagian berikut.

## Risiko sisa

Tiga hal ini tidak bisa diselesaikan lewat DNS, dan tetap berlaku setelah
checklist selesai:

1. **PTR kosong.** `43.165.198.116` tidak punya reverse DNS. Hanya Sumopod yang
   bisa mengaturnya. Minta dukungan Sumopod menambahkan PTR, atau pindah ke
   layanan yang punya PTR sendiri.
2. **Relay bersama.** Reputasi IP dibagi dengan semua pengguna Sumopod. Satu
   pengirim spam di IP yang sama menurunkan skor email kamu.
3. **Domain baru.** Reputasi dibangun dari waktu dan volume. Kirim volume kecil
   dulu. Jangan kirim massal pada minggu pertama.

Jika setelah semua langkah email masih masuk spam, jalur perbaikan yang tersisa
hanya dua: minta Sumopod memberi IP khusus dengan PTR, atau pindah ke penyedia
API email transaksional yang menandatangani pesan dan punya PTR.

## Perkiraan hasil

| Kondisi | Perkiraan posisi di Gmail |
| --- | --- |
| Sebelum checklist (sekarang) | Spam, hampir pasti |
| Setelah SPF + DKIM + DMARC, PTR masih kosong | Campur, masih sering spam |
| Setelah semua langkah dan reputasi naik | Inbox |

Tabel ini perkiraan, bukan jaminan. Filter Gmail tidak dipublikasikan.
