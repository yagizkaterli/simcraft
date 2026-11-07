# count-rerun (HRK-046)

Son merge PR'larin **PR govdesinde belgelenen komutlarini**, her satir icin pinned
`head_sha`'da **bos bir temp klona** kosar ve ayni kosumdan iki ozdes artefakt uretir:

- `re-run.csv`
- `re-run.md`
- `re-run-out/<repo>_<pr>.runN.log` (satir basina komut cikti logu = artifact)

## Kullanim

```sh
go build -o count-rerun .
./count-rerun <girdi.json> [cikti-onek]        # varsayilan onek: re-run
```

Girdi bicimi:

```json
[{"r":"simcraft","n":3,"t":"<sha-kisaltma>","b":"<PR govdesi>","art":"opsiyonel/beklenen/artifact"}]
```

`art` verilirse PASS icin o dosya diskte **ve bu kosumda yazilmis** olmalidir;
verilmezse aracin yazdigi komut cikti logu artifact sayilir.

Ortam degiskenleri:

| Degisken | Varsayilan | Anlam |
|---|---|---|
| `COUNTRERUN_PRIVATE` | `run` | `skip` → private satirlar kosulmaz, `DOES-NOT-RESOLVE` + neden yazilir |
| `COUNTRERUN_CLEAN_BETWEEN` | `0` | `1` → timed kosular arasinda derleme cache'i temizlenir (Rust'ta 3x rebuild) |

## Kolonlar

`repo, pr, head_sha, command, access, runs, per_run_values, median_seconds, rc, result, artifact, reason_if_unresolved`

## Degismezler (falsifier)

- `result` yalniz `PASS | FAIL | DOES-NOT-RESOLVE`.
- `PASS` = `rc 0` **ve** beklenen artifact diskte **ve** bu kosumda yazilmis.
  `rc 0` + bos cikti `PASS` degildir.
- Kimlik / ozel bagimlilik / host yolu / yetki engeli = `DOES-NOT-RESOLVE` + neden; asla `FAIL`.
- `head_sha` = klonlanan sha; receipt blogu `clone_sha` ile dogrular (`sha_match`).
- Manset sayilar ciktinin ilk satirlari: `kosulabilir/private/komutsuz` ve `PASS/FAIL/DOES-NOT-RESOLVE`.
- Medyanin yaninda ham per-run degerler ve `n` gorunur.
- Canli repoyu degistiren komutlar (`gh pr merge`, `git push`, `--force`, `-merge=true`)
  kosulmaz: `DOES-NOT-RESOLVE` (kimlik/yetki engeli).
- Sir yoktur; private klon kimligi yalniz kendi runner'imizda bulunur.

## CI

`.github/workflows/rerun.yml` (manuel tetikleme) public satirlari kosar ve `re-run.csv` +
`re-run.md` artefaktlarini yukler. Private satirlar ayni binary ile **kendi runner'imizda**
(`COUNTRERUN_PRIVATE=run`) kosulur; public CI'da `skip` verilir.
