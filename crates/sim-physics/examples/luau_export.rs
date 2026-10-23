//! luau-export — pist RON'unu Roblox/Luau modülüne aktarır.
//!
//!   cargo run --example luau_export -- games/roadcase/tracks/roadcase-highway.ron out.luau
//!
//! Geometri `sim_physics::luau::emit` içinde `Track::pose` / `Arm::pose` ile örneklenir;
//! deterministik (aynı RON → aynı bayt).

use sim_physics::{Track, TrackDef};

fn main() {
    let mut args = std::env::args().skip(1);
    let (Some(ron_path), Some(out_path)) = (args.next(), args.next()) else {
        eprintln!("usage: luau_export <track.ron> <out.luau>");
        std::process::exit(2);
    };

    let raw = std::fs::read_to_string(&ron_path).unwrap_or_else(|e| {
        eprintln!("{ron_path}: {e}");
        std::process::exit(2);
    });
    let def: TrackDef = ron::from_str(&raw).unwrap_or_else(|e| {
        eprintln!("{ron_path}: RON cozulemedi: {e}");
        std::process::exit(3);
    });
    let track = Track::new(&def).unwrap_or_else(|problems| {
        eprintln!("pist gecersiz: {}", problems.join("; "));
        std::process::exit(3);
    });

    let text = sim_physics::luau::emit(&track, &ron_path);
    std::fs::write(&out_path, &text).unwrap_or_else(|e| {
        eprintln!("{out_path}: {e}");
        std::process::exit(2);
    });
    println!(
        "yazildi {out_path} · pist={} · uzunluk={:.3} m · satir={}",
        track.name,
        track.length.0 as f64 / 65536.0,
        text.lines().count()
    );
}
