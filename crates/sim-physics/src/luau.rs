//! Luau export: a track as a Roblox `ModuleScript`.
//!
//! Geometry is never re-laid here: sampling goes through `Track::pose` / `Arm::pose`, the same
//! integer arithmetic the simulation uses. Output is deterministic (fixed step, fixed formatting,
//! fixed key order, no wall clock), so the same RON yields the same bytes.

use crate::{Angle, Fx, Track};
use std::fmt::Write as _;

const STEP_M: i64 = 10;

fn metres(fx: Fx) -> f64 {
    fx.0 as f64 / 65536.0
}

fn degrees(a: Angle) -> f64 {
    a.0 as f64 * 360.0 / (1u64 << 32) as f64
}

fn sample(mut pose: impl FnMut(Fx) -> (f64, f64, f64), length: Fx, out: &mut String) {
    let total_mm = (length.0 as i128 * 1000 / 65536) as i64;
    let mut s_mm = 0i64;
    loop {
        let (x, y, h) = pose(Fx::ratio(s_mm, 1000));
        writeln!(out, "    {{ {x:.3}, {y:.3}, {h:.3} }},").expect("yazilir");
        if s_mm >= total_mm {
            break;
        }
        s_mm = (s_mm + STEP_M * 1000).min(total_mm);
    }
}

/// Emit a Luau module for `track`, tagged with the source path it came from.
pub fn emit(track: &Track, ron_path: &str) -> String {
    let mut out = String::new();
    writeln!(out, "-- {}.luau — simcraft'tan uretildi; elle duzenlemeyin.", track.name).unwrap();
    writeln!(out, "-- kaynak: {ron_path}").unwrap();
    writeln!(out, "return {{").unwrap();
    writeln!(out, "  schema = \"roadcase-track/1\",").unwrap();
    writeln!(out, "  name = {:?},", track.name).unwrap();
    writeln!(out, "  width_m = {:.3},", metres(track.width)).unwrap();
    writeln!(out, "  closed = {},", track.closed).unwrap();
    writeln!(out, "  length_m = {:.3},", metres(track.length)).unwrap();
    writeln!(out, "  step_m = {STEP_M}.000,").unwrap();
    writeln!(out, "  centerline = {{").unwrap();
    sample(
        |s| {
            let p = track.pose(s, Fx::ZERO);
            (metres(p.x), metres(p.y), degrees(p.heading))
        },
        track.length,
        &mut out,
    );
    writeln!(out, "  }},").unwrap();
    match &track.fork {
        None => writeln!(out, "  fork = nil,").unwrap(),
        Some(fork) => {
            writeln!(out, "  fork = {{").unwrap();
            writeln!(out, "    at_m = {:.3},", metres(fork.at)).unwrap();
            writeln!(out, "    arms = {{").unwrap();
            for arm in &fork.arms {
                writeln!(out, "      {{").unwrap();
                writeln!(out, "        name = {:?},", arm.name).unwrap();
                writeln!(out, "        width_m = {:.3},", metres(arm.width)).unwrap();
                writeln!(out, "        length_m = {:.3},", metres(arm.length)).unwrap();
                writeln!(out, "        end_wall = {},", arm.end_wall).unwrap();
                writeln!(out, "        left_wall_m = {},", wall(arm.wall_left)).unwrap();
                writeln!(out, "        right_wall_m = {},", wall(arm.wall_right)).unwrap();
                writeln!(out, "        centerline = {{").unwrap();
                sample(
                    |s| {
                        let p = arm.pose(s, Fx::ZERO);
                        (metres(p.x), metres(p.y), degrees(p.heading))
                    },
                    arm.length,
                    &mut out,
                );
                writeln!(out, "        }},").unwrap();
                writeln!(out, "      }},").unwrap();
            }
            writeln!(out, "    }},").unwrap();
            writeln!(out, "  }},").unwrap();
        }
    }
    writeln!(out, "}}").unwrap();
    out
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::TrackDef;

    fn roadcase() -> Track {
        let root = std::path::Path::new(env!("CARGO_MANIFEST_DIR")).join("../..");
        let raw = std::fs::read_to_string(root.join("games/roadcase/tracks/roadcase-highway.ron"))
            .expect("roadcase pisti okunur");
        let def: TrackDef = ron::from_str(&raw).expect("RON cozulur");
        Track::new(&def).expect("pist gecerli")
    }

    /// Golden: committed Luau artefakti, ayni RON'dan yeniden uretilen baytlarla ayni olmali.
    #[test]
    fn roadcase_luau_matches_committed_artifact() {
        let got = emit(&roadcase(), "games/roadcase/tracks/roadcase-highway.ron");
        let root = std::path::Path::new(env!("CARGO_MANIFEST_DIR")).join("../..");
        let want = std::fs::read_to_string(root.join("games/roadcase-roblox/roadcase_track.luau"))
            .expect("committed luau artefakti okunur");
        assert_eq!(got, want, "luau cikisi artefaktan ayristi; yeniden uret ve commit et");
    }

    #[test]
    fn geometry_survives_the_export() {
        let t = roadcase();
        let luau = emit(&t, "x.ron");
        assert!(luau.contains("length_m = 2360.146"));
        assert!(luau.contains("at_m = 2360.146"));
        assert!(luau.contains("length_m = 746.594"), "HIGHWAY 9 uzunlugu");
        assert!(luau.contains("length_m = 337.077"), "MOTEL EXIT uzunlugu");
        assert!(luau.contains("closed = false"));
        assert_eq!(emit(&t, "x.ron"), luau, "iki uretim ayni bayt");
    }
}

fn wall(w: Option<Fx>) -> String {
    match w {
        Some(f) => format!("{:.3}", metres(f)),
        None => "nil".into(),
    }
}
