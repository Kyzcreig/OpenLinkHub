# Fork notes — Kyzcreig/OpenLinkHub

This is a fork of [jurkovic-nikola/OpenLinkHub](https://github.com/jurkovic-nikola/OpenLinkHub)
with two additions developed against a real iCUE LINK System Hub on **firmware 3.10.636**. `main`
tracks upstream; the work lives on the branches below.

> ⚠️ **Use at your own risk.** These changes were reverse-engineered and verified on **one** hardware
> setup (a LINK System Hub on fw 3.10.636 driving 10× LX/RX fans + a TITAN pump + an LS350 Aurora
> strip on the LINK adapter). The upstream maintainer runs a similar rig **without** needing the #452
> fix, so the firmware can clearly drive LEDs as-is on some setups — meaning the dark-fans bug is
> setup/firmware-revision specific, not universal. If upstream works for you, **use upstream.** Only
> reach for this fork if you hit the exact symptom below.

---

## 1. Fix: fans/pump stay dark on fw 3.10.636 (issue #452)

**Branch:** [`fix/452-fw31-1122byte-frame`](../../tree/fix/452-fw31-1122byte-frame) ·
**Upstream issue:** [#452](https://github.com/jurkovic-nikola/OpenLinkHub/issues/452)

**Symptom:** OpenLinkHub runs, every device is detected, brightness/profile/colors are all provably
correct, RPM/coolant telemetry works — but the fans and pump LEDs are **completely dark**, while the
same hub lights perfectly under Windows iCUE/SignalRGB on the same firmware. There is **no**
`Connection timed out` / `Unable to write` error in `stdout.log` (that error is a *different*,
power-cycle-fixable wedge — this is not that).

**Test hardware:** iCUE LINK System Hub `1b1c:0c3f`, `bcdDevice 1.00`, fw 3.10.636, connected by **cable to an internal USB header** (enumerates under the motherboard AMD xHCI root hub — not a PCIe-connector hub). Cluster: TITAN 360 AIO pump + 6x LX + 3x RX fans + a LINK-adapter RGB strip (the 204-LED strip region + 170-LED fan region that make up the 374-LED frame).

**Root cause:** on this firmware the hub wants a **1122-byte LED frame** = one **374-LED** colour
buffer split **204 (LINK-adapter strip zone) | 170 (fan zone)**, with **both** zones carrying real
per-LED colour. Stock builds emit short frames and the fan zone never gets written. (An earlier
analysis misread the fan zone's bytes as a fixed `5d dd 32` "terminator" — it isn't; that was just
the fan colour in every reference capture happening to be green.)

**Build & run:**
```bash
git clone https://github.com/Kyzcreig/OpenLinkHub.git
cd OpenLinkHub
git checkout fix/452-fw31-1122byte-frame
go build .
go test ./src/devices/lsh/ -run Frame   # regression guard for the 374-slot frame
# install over your existing binary (back up the stock one first):
sudo systemctl stop OpenLinkHub
sudo cp /opt/OpenLinkHub/OpenLinkHub /opt/OpenLinkHub/OpenLinkHub.stock.bak
sudo cp ./OpenLinkHub /opt/OpenLinkHub/OpenLinkHub
sudo systemctl start OpenLinkHub
```

**Verify it's actually working** (don't trust the API success message — read the wire): a Linux
`usbmon` capture should show full **1122-byte** `0x0464` frames with distinct per-slot colours, and
commanding **black `(0,0,0)`** should turn every zone off (if a zone stays lit on black, that zone
isn't being written). The fix ships with a regression test asserting the fan zone is never a hardcoded
literal.

---

## 2. Effect: `teal-pink-hue-cycle` (no white flash)

**Branches:** [`feat/teal-pink-hue-cycle`](../../tree/feat/teal-pink-hue-cycle) (on top of the #452
fix, the way I actually run it) · [`feat/tpc-standalone`](../../tree/feat/tpc-standalone) (cherry-picked
clean off upstream `main`, **no #452 dependency in the diff** — this is what PR
[#457](https://github.com/jurkovic-nikola/OpenLinkHub/pull/457) proposes) ·
**Upstream PR:** [#457](https://github.com/jurkovic-nikola/OpenLinkHub/pull/457)

A flowing **teal → pink** hue-arc effect that breathes across the fans/strip. It sweeps a bounded hue
arc at **full saturation** (sine ping-pong, per-LED phase offset), so it never blends straight through
the desaturated centre of the colour space — i.e. **no white/washed flash** at the midpoint, which is
the failure mode of a naive RGB lerp between the two colours.

Implemented as a native Go effect (`src/rgb/tealpinkcycle.go`), wired into the `rgbModes` whitelist and
the `generateRgbEffect` switch like any built-in effect, and configured through the normal
start/end-colour API. It ships its **own** correct float HSV→RGB conversion rather than the package's
`HsvToRgb` helper.

> **Note for maintainer / other contributors:** the package `HsvToRgb` in `src/rgb/rotator.go` has an
> integer-division bug in its sector term (`(h/60)*2`) that produces hue-shifted output for most hues.
> This effect deliberately does **not** use it (ships its own float conversion) to avoid changing the
> behaviour of `rotator`, its only caller. Flagging it here rather than drive-by-fixing it, since the
> rotator effect may silently depend on the current output. See PR #457 for details.

**Build & run:**
```bash
git checkout feat/tpc-standalone   # or feat/teal-pink-hue-cycle if you also need the #452 fix
go build .
# install as above, then assign the effect via the web UI (http://localhost:27003)
# or POST /api/color with profile "teal-pink-hue-cycle".
```

---

*Maintained loosely as a personal fork. PRs to upstream are preferred where they'll land; these
branches exist so the working code is available to anyone hitting the same firmware behaviour.*
