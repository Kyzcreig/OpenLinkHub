# Fork notes — Kyzcreig/OpenLinkHub

This is a fork of [jurkovic-nikola/OpenLinkHub](https://github.com/jurkovic-nikola/OpenLinkHub)
with a custom teal-pink RGB effect and one now-superseded #452 investigation branch developed against a real iCUE LINK System Hub on **firmware 3.10.636**. `main`
tracks upstream; the work lives on the branches below.

> ⚠️ **Use upstream unless you have a specific reason not to.** The `teal-pink-hue-cycle` effect is
> the useful addition here. The older #452 "1122-byte frame" branch is now considered **investigation
> only**: the likely miss was that the iCUE LINK adapter strip was not activated in the OpenLinkHub
> dashboard (`ExternalAdapter {"1":0}`). After selecting **adapter ID 10 = `iCUE LINK 5000T RGB`**, stock
> OpenLinkHub sees the 204 case-strip LEDs correctly. Retest is pending visual confirmation from the
> owner, so do **not** treat the #452 branch as a required firmware fix.

---

## 1. Superseded investigation: dark LEDs on fw 3.10.636 (issue #452)

**Branch:** [`fix/452-fw31-1122byte-frame`](../../tree/fix/452-fw31-1122byte-frame) ·
**Upstream issue:** [#452](https://github.com/jurkovic-nikola/OpenLinkHub/issues/452)

**Current status (2026-06-18): likely dashboard configuration, not a firmware/write-size bug.** The
OpenLinkHub profile originally had the LINK Adapter set to `None`:

```json
"ExternalAdapter": { "1": 0 }
```

For ACE-AI's Corsair iCUE 5000T case, the correct dashboard choice is **adapter ID 10 =
`iCUE LINK 5000T RGB`**. After setting it via `/api/hub/linkAdapter`, the live device object exposes
204 LEDs across six case-strip subdevices:

- Top Left Strip: 32
- Front Left Strip: 38
- Bottom Left Strip: 32
- Bottom Right Strip: 32
- Front Right Strip: 38
- Top Right Strip: 32

A short stock-binary retest with adapter ID 10 in place showed stock OpenLinkHub 0.8.8 starts cleanly,
sees the 204 adapter LEDs, accepts static RGB writes, and keeps pump telemetry healthy. The remaining
missing proof is **physical visual confirmation** from the owner that stock+adapter-configured lights
the rig; until then, this branch should be treated as a debugging artifact, not a recommended fix.

**Test hardware:** iCUE LINK System Hub `1b1c:0c3f`, `bcdDevice 1.00`, fw 3.10.636, connected by
**cable to an internal USB header** (enumerates under the motherboard AMD xHCI root hub — not a
PCIe-connector hub). Cluster: TITAN 360 AIO pump + 6× LX + 3× RX fans + iCUE LINK 5000T RGB case
strip system (204-LED adapter region + 170-LED fan/pump region).

**Do this before trying any fork branch:** open the OpenLinkHub dashboard, find the `iCUE LINK ADAPTER`
channel, and select the actual attached strip/case device (for ACE-AI: `iCUE LINK 5000T RGB`). Then
set a stock profile such as `static` and visually verify the LEDs.

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
the `generateRgbEffect` switch like any built-in effect, registered as a first-class profile in
`database/rgb.json` (so `GetRgbProfile` resolves it for all users and the RGB Editor shows its
start/end colour pickers), and added to `rgbProfileUpgrade` in `lsh.go` (so existing users' per-device
rgb DBs get the profile injected on startup). It ships its **own** correct float HSV→RGB conversion
rather than the package's `HsvToRgb` helper.

> **PR #457 was closed by the maintainer (2026-06-19).** His feedback: the original PR registered only
> the render code, so on a clean install the profile never upgraded into the device DB (not selectable/
> configurable, no RGB-Editor colour pickers), and he noted the look is approximable with the newer
> `arc` mode. The registration gap is now **fixed on both branches** (the `database/rgb.json` +
> `rgbProfileUpgrade` additions above), verified end-to-end on real hardware: simulate a fresh user by
> removing the profile from the device DB, restart, and `upgradeRgbProfile` re-injects it
> (`Upgrading RGB profile profile=teal-pink-hue-cycle` in the log) with correct teal/pink colours. Note
> on the `arc` overlap: `arc` in **random** mode is a full rainbow, and in **colour** mode it uses a
> linear RGB lerp (`lerpColor`) which reintroduces the desaturated-midpoint white flash this effect was
> built to avoid — so it's close but not an exact substitute for a bounded teal↔pink no-flash sweep.

> **Note for maintainer / other contributors:** the package `HsvToRgb` in `src/rgb/rotator.go` has an
> integer-division bug in its sector term (`(h/60)*2`) that produces hue-shifted output for most hues.
> This effect deliberately does **not** use it (ships its own float conversion) to avoid changing the
> behaviour of `rotator`, its only caller. Flagging it here rather than drive-by-fixing it, since the
> rotator effect may silently depend on the current output. See PR #457 for details.

**Build & run:**
```bash
git checkout feat/tpc-standalone   # preferred; no #452 dependency
go build .
# install as above, then assign the effect via the web UI (http://localhost:27003)
# or POST /api/color with profile "teal-pink-hue-cycle".
```

---

*Maintained loosely as a personal fork. PRs to upstream are preferred where they'll land; these
branches exist so the working code is available to anyone hitting the same firmware behaviour.*
