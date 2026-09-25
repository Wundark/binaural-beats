// Session timeline: the beat frequency over the brainwave bands, the tone and
// pink noise volumes below it, and a playhead. Clicking or dragging (mouse or
// touch) seeks.

const BANDS = [
  { name: "Delta", from: 0, to: 4 },
  { name: "Theta", from: 4, to: 8 },
  { name: "Alpha", from: 8, to: 13 },
  { name: "Beta", from: 13, to: 30 },
  { name: "Gamma", from: 30, to: Infinity },
];

const PAD = { left: 8, right: 8, top: 6, bottom: 16 };
const LANE_GAP = 6;
const LANE_FRACTION = 0.22; // share of the plot height for the volume lane

// Value of key at time t, interpolated linearly between changes like the engine.
export function valueAt(changes, t, key) {
  if (changes.length === 0) return 0;
  if (t <= changes[0].time) return changes[0][key];
  for (let i = 0; i < changes.length - 1; i++) {
    const a = changes[i];
    const b = changes[i + 1];
    if (t >= a.time && t < b.time) {
      return a[key] + ((b[key] - a[key]) * (t - a.time)) / (b.time - a.time);
    }
  }
  return changes[changes.length - 1][key];
}

// A tick spacing (in seconds) that gives roughly 4-8 ticks.
function tickStep(total) {
  const steps = [60, 120, 300, 600, 900, 1800, 3600, 7200];
  return steps.find((s) => total / s <= 8) || 14400;
}

function formatTick(seconds) {
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  return h > 0 ? `${h}:${m.toString().padStart(2, "0")}` : `${m}m`;
}

export class Timeline {
  constructor(canvas, { onSeek, onHover }) {
    this.canvas = canvas;
    this.onSeek = onSeek;
    this.onHover = onHover;
    this.data = null;
    this.time = 0;
    this.hoverX = null;
    this.preview = null; // time shown by the playhead while scrubbing
    this.dragging = null; // pointer id of a drag in progress

    new ResizeObserver(() => this.draw()).observe(canvas);
    canvas.addEventListener("pointerdown", (e) => {
      if (!this.data || (e.pointerType === "mouse" && e.button !== 0)) return;
      this.dragging = e.pointerId;
      canvas.setPointerCapture(e.pointerId);
      this.scrub(e);
    });
    canvas.addEventListener("pointermove", (e) => {
      if (this.dragging === null && e.pointerType !== "mouse") return;
      this.scrub(e);
    });
    canvas.addEventListener("pointerup", (e) => {
      if (this.dragging !== e.pointerId) return;
      const t = this.timeAtEvent(e);
      this.endDrag(e.pointerType !== "mouse");
      if (t !== null) this.onSeek(t);
    });
    // A touch that turns into a page scroll cancels the drag.
    canvas.addEventListener("pointercancel", () => this.endDrag(true));
    canvas.addEventListener("pointerleave", (e) => {
      if (e.pointerType === "mouse" && this.dragging === null) this.endDrag(true);
    });
    // Long-press would otherwise open a context menu on touch screens.
    canvas.addEventListener("contextmenu", (e) => e.preventDefault());
  }

  // Follow the pointer: hover details, and while dragging, the playhead.
  scrub(e) {
    this.hoverX = e.offsetX;
    if (this.dragging !== null) this.preview = this.timeAtEvent(e);
    this.reportHover();
    this.draw();
  }

  endDrag(clearHover) {
    this.dragging = null;
    this.preview = null;
    if (clearHover) {
      this.hoverX = null;
      this.onHover(null);
    }
    this.draw();
  }

  // Show the playhead at t (or back at the playing time, for null) without
  // seeking, e.g. while a slider is dragged.
  setPreview(t) {
    this.preview = t;
    this.draw();
  }

  setData(timeline) {
    this.data = timeline && timeline.changes.length ? timeline : null;
    this.canvas.classList.toggle("seekable", !!this.data);
    this.draw();
  }

  setTime(t) {
    this.time = t;
    this.draw();
  }

  plotWidth() {
    return this.canvas.clientWidth - PAD.left - PAD.right;
  }

  timeAtEvent(e) {
    if (!this.data) return null;
    const f = (e.offsetX - PAD.left) / this.plotWidth();
    return Math.min(1, Math.max(0, f)) * this.data.total_duration;
  }

  reportHover() {
    if (!this.data || this.hoverX === null) return;
    const t = this.timeAtEvent({ offsetX: this.hoverX });
    const beat = Math.abs(valueAt(this.data.changes, t, "beat_frequency"));
    const band = BANDS.find((b) => beat < b.to) || BANDS[BANDS.length - 1];
    this.onHover({ time: t, beat, band: band.name });
  }

  draw() {
    const canvas = this.canvas;
    const dpr = window.devicePixelRatio || 1;
    const w = canvas.clientWidth;
    const h = canvas.clientHeight;
    if (w === 0 || h === 0) return;
    if (canvas.width !== Math.round(w * dpr) || canvas.height !== Math.round(h * dpr)) {
      canvas.width = Math.round(w * dpr);
      canvas.height = Math.round(h * dpr);
    }
    const ctx = canvas.getContext("2d");
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    ctx.clearRect(0, 0, w, h);

    const css = getComputedStyle(document.documentElement);
    const color = (name) => css.getPropertyValue(name).trim();
    const accent = color("--accent");
    const muted = color("--text-secondary");
    const text = color("--text-primary");
    const tone = color("--success");
    const bg = color("--bg-secondary");

    const plotW = w - PAD.left - PAD.right;
    const plotH = h - PAD.top - PAD.bottom;
    const laneH = Math.round(plotH * LANE_FRACTION);
    const beatH = plotH - laneH - LANE_GAP;
    const beatTop = PAD.top;
    const laneTop = beatTop + beatH + LANE_GAP;

    ctx.fillStyle = bg;
    ctx.fillRect(PAD.left, beatTop, plotW, beatH);
    ctx.fillRect(PAD.left, laneTop, plotW, laneH);

    ctx.font = "10px -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif";
    if (!this.data) {
      ctx.fillStyle = muted;
      ctx.textAlign = "center";
      ctx.textBaseline = "middle";
      ctx.fillText("Load a session to see its timeline", PAD.left + plotW / 2, beatTop + beatH / 2);
      return;
    }

    const { changes, total_duration: total } = this.data;
    const maxBeat = Math.max(...changes.map((c) => Math.abs(c.beat_frequency)));
    const yMax = Math.max(16, Math.ceil((maxBeat * 1.15) / 2) * 2);
    const x = (t) => PAD.left + (total > 0 ? (t / total) * plotW : 0);
    const yBeat = (f) => beatTop + beatH - (Math.min(f, yMax) / yMax) * beatH;
    const yLane = (v) => laneTop + laneH - v * laneH;

    // Brainwave bands, alternating shades, labelled on the left.
    BANDS.forEach((band, i) => {
      if (band.from >= yMax) return;
      const top = yBeat(Math.min(band.to, yMax));
      const bottom = yBeat(band.from);
      ctx.fillStyle = i % 2 ? "rgba(255,255,255,0.035)" : "rgba(255,255,255,0)";
      ctx.fillRect(PAD.left, top, plotW, bottom - top);
      if (bottom - top >= 12) {
        ctx.fillStyle = muted;
        ctx.globalAlpha = 0.7;
        ctx.textAlign = "left";
        ctx.textBaseline = "middle";
        ctx.fillText(`${band.name} ${band.from}${band.to === Infinity ? "+" : `-${band.to}`} Hz`, PAD.left + 6, (top + bottom) / 2);
        ctx.globalAlpha = 1;
      }
    });

    // Time ticks
    const step = tickStep(total);
    ctx.strokeStyle = "rgba(255,255,255,0.06)";
    ctx.fillStyle = muted;
    ctx.textAlign = "center";
    ctx.textBaseline = "top";
    for (let t = step; t < total; t += step) {
      const tx = Math.round(x(t)) + 0.5;
      ctx.beginPath();
      ctx.moveTo(tx, beatTop);
      ctx.lineTo(tx, laneTop + laneH);
      ctx.stroke();
      ctx.fillText(formatTick(t), tx, laneTop + laneH + 3);
    }

    // Elapsed shading
    const px = x(Math.min(this.preview ?? this.time, total));
    ctx.fillStyle = "rgba(108,99,255,0.10)";
    ctx.fillRect(PAD.left, beatTop, px - PAD.left, beatH);
    ctx.fillRect(PAD.left, laneTop, px - PAD.left, laneH);

    // A series is linear between changes and flat outside them.
    const points = (key, map) => {
      const pts = [[PAD.left, map(changes[0][key])]];
      for (const c of changes) pts.push([x(c.time), map(c[key])]);
      pts.push([PAD.left + plotW, map(changes[changes.length - 1][key])]);
      return pts;
    };
    const area = (pts, base, fill) => {
      ctx.beginPath();
      ctx.moveTo(pts[0][0], base);
      for (const [px2, py] of pts) ctx.lineTo(px2, py);
      ctx.lineTo(pts[pts.length - 1][0], base);
      ctx.closePath();
      ctx.fillStyle = fill;
      ctx.fill();
    };
    const line = (pts, stroke, width) => {
      ctx.beginPath();
      pts.forEach(([px2, py], i) => (i ? ctx.lineTo(px2, py) : ctx.moveTo(px2, py)));
      ctx.strokeStyle = stroke;
      ctx.lineWidth = width;
      ctx.lineJoin = "round";
      ctx.stroke();
      ctx.lineWidth = 1;
    };

    // Volume lane: pink noise (grey) and tone (green)
    const tonePts = points("tone_volume", yLane);
    area(tonePts, laneTop + laneH, "rgba(76,175,80,0.2)");
    line(tonePts, tone, 1.5);
    ctx.setLineDash([4, 3]);
    line(points("pink_noise_volume", yLane), muted, 1.5);
    ctx.setLineDash([]);

    // Beat frequency
    const beatPts = changes.length ? points("beat_frequency", (f) => yBeat(Math.abs(f))) : [];
    line(beatPts, accent, 2.5);

    // Playhead
    ctx.strokeStyle = text;
    ctx.lineWidth = 1.5;
    ctx.beginPath();
    ctx.moveTo(Math.round(px) + 0.5, beatTop);
    ctx.lineTo(Math.round(px) + 0.5, laneTop + laneH);
    ctx.stroke();
    ctx.lineWidth = 1;

    // Hover crosshair
    if (this.hoverX !== null && this.hoverX >= PAD.left && this.hoverX <= PAD.left + plotW) {
      ctx.strokeStyle = "rgba(255,255,255,0.35)";
      ctx.setLineDash([3, 3]);
      ctx.beginPath();
      ctx.moveTo(Math.round(this.hoverX) + 0.5, beatTop);
      ctx.lineTo(Math.round(this.hoverX) + 0.5, laneTop + laneH);
      ctx.stroke();
      ctx.setLineDash([]);
    }
  }
}
