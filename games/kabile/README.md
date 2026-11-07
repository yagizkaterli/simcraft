# kabile — HERAKLES SIM dünyası, simcraft üzerinde

`kabile-16x16-v1`, HERAKLES SIM'in (hos-sim, Rust) `sim-fiziksel/kabile` koşumunun
simcraft oyunu olarak yeniden sahnelenişidir. Dünya düzeni, 12 aktörün başlangıç
konumları ve 119 satırlık niyet betiği `game.ron`'a birebir işlendi:

- Her aktör bir FSM zinciri: tick başına bir geçiş, geçişte bir `Move`.
- `solid: true` aktörler → "at most one solid per cell": a4'ün a5 üstüne adımı
  motor tarafından engellenir (hos-sim'deki CakismaYok/IHLAL ile aynı sonuç).
- a1'in dünya dışına adımı (x=0'dan -1) engellenir (SinirIcinde/IHLAL).
- a5 iki tick bekler; böylece a4'ün çakışma denemesi, a5 yerindeyken engellenir
  (ardışık betikteki sıralamayla aynı).
- a11/a12 yetkisizdir; sim'de hiç hareket etmediler — burada da hareketsiz
  (`ant_dormant`).

## Koşum (kanıtlanmış)

```bash
# doğrulama: rc=0, "loads, its views check out, 300 ticks with presses, no complaint"
simcraft-check games/kabile
# 2.5D HD kare (deterministik: iki koşum bayt-özdeş, sha f971f6c8…)
simcraft-play games/kabile --shot kabile-t15.png --size 1280x720 --ticks 15
simcraft-play games/kabile --shot kabile-wide.png --size 1280x720 --ticks 15 --camera wide
```

## Kapsam / sınır

- v0: konumlar ve engellenmeler birebir; RED *anları* animasyon olarak yok
  (yetkisizlik yalnız duruşla gösterilir).
- Birebir **sıra** (119 satırın ardışık işlenmesi) ileride `--replay` hattıyla
  (acts + hash) canlandırılacak; bu sürüm tick-senkron FSM zinciridir.
- Kaynak: `kovan/HRK-HARNESS-SIM-RUST-001/sim-fiziksel/kabile-*` (hos-sim koşumu,
  son_digest `e54848d29976…`), makbuz: `.hterm/work/HRK-SIMCRAFT-KABILE-001.receipt.json`.
