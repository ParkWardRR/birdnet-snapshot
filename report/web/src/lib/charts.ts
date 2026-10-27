// Small chart maths: curves, the streamgraph baseline and tidy ticks. No
// dependencies, so the report stays a handful of kilobytes.
export type Pt = [number, number];

const f = (n: number) => Math.round(n * 100) / 100;

/**
 * A smooth curve through every point that never overshoots (monotone cubic,
 * Fritsch–Butland), so a peak keeps its height. Returns "M…C…".
 */
export function monotonePath(pts: Pt[]): string {
	const n = pts.length;
	if (n < 2) return '';
	const h: number[] = [];
	const m: number[] = [];
	for (let i = 0; i < n - 1; i++) {
		h.push(pts[i + 1][0] - pts[i][0]);
		m.push((pts[i + 1][1] - pts[i][1]) / h[i]);
	}
	const t: number[] = new Array(n);
	t[0] = m[0];
	t[n - 1] = m[n - 2];
	for (let i = 1; i < n - 1; i++) {
		t[i] =
			m[i - 1] * m[i] <= 0
				? 0
				: (3 * (h[i - 1] + h[i])) /
					((2 * h[i] + h[i - 1]) / m[i - 1] + (h[i] + 2 * h[i - 1]) / m[i]);
	}
	let d = `M${f(pts[0][0])},${f(pts[0][1])}`;
	for (let i = 0; i < n - 1; i++) {
		const [x0, y0] = pts[i];
		const [x1, y1] = pts[i + 1];
		const dx = h[i] / 3;
		d += `C${f(x0 + dx)},${f(y0 + t[i] * dx)} ${f(x1 - dx)},${f(y1 - t[i + 1] * dx)} ${f(x1)},${f(y1)}`;
	}
	return d;
}

/**
 * A uniform cubic B-spline (d3's curveBasis): smoother than the monotone
 * curve and always inside the hull of its points, so the edges of stacked
 * bands, which are ordered at every point, can never cross between points.
 * Returns "M…C…L…".
 */
export function basisPath(pts: Pt[]): string {
	let x0 = 0,
		y0 = 0,
		x1 = 0,
		y1 = 0,
		s = 0,
		d = '';
	const bez = (x: number, y: number) => {
		d += `C${f((2 * x0 + x1) / 3)},${f((2 * y0 + y1) / 3)} ${f((x0 + 2 * x1) / 3)},${f((y0 + 2 * y1) / 3)} ${f((x0 + 4 * x1 + x) / 6)},${f((y0 + 4 * y1 + y) / 6)}`;
	};
	for (const [x, y] of pts) {
		if (s === 0) {
			s = 1;
			d += `M${f(x)},${f(y)}`;
		} else if (s === 1) {
			s = 2;
		} else if (s === 2) {
			s = 3;
			d += `L${f((5 * x0 + x1) / 6)},${f((5 * y0 + y1) / 6)}`;
			bez(x, y);
		} else {
			bez(x, y);
		}
		x0 = x1;
		x1 = x;
		y0 = y1;
		y1 = y;
	}
	if (s === 3) bez(x1, y1);
	if (s >= 2) d += `L${f(x1)},${f(y1)}`;
	return d;
}

/**
 * The streamgraph baseline (Byron & Wattenberg's "wiggle", as in d3): the
 * bottom edge of the first layer at each column, chosen so the silhouette
 * changes slope as little as possible. `v[layer][column]` are raw values.
 */
export function wiggle(v: number[][]): number[] {
	const n = v.length;
	const m = n ? v[0].length : 0;
	const base = new Array<number>(m).fill(0);
	let y = 0;
	for (let j = 1; j < m; j++) {
		let s1 = 0;
		let s2 = 0;
		for (let i = 0; i < n; i++) {
			const cur = v[i][j];
			let s3 = (cur - v[i][j - 1]) / 2;
			for (let k = 0; k < i; k++) s3 += v[k][j] - v[k][j - 1];
			s1 += cur;
			s2 += s3 * cur;
		}
		base[j - 1] = y;
		if (s1) y -= s2 / s1;
	}
	if (m) base[m - 1] = y;
	return base;
}

/** A round step (1, 2 or 5 × 10ⁿ) giving about `target` intervals over `range`. */
export function niceStep(range: number, target: number): number {
	const raw = Math.max(range, 1e-9) / Math.max(target, 1);
	const mag = 10 ** Math.floor(Math.log10(raw));
	const r = raw / mag;
	return (r < 1.5 ? 1 : r < 3.5 ? 2 : r < 7.5 ? 5 : 10) * mag;
}

/** Ticks at multiples of `step` from `lo` to `hi`, inclusive. */
export function ticks(lo: number, hi: number, step: number): number[] {
	const out: number[] = [];
	for (let v = Math.ceil(lo / step) * step; v <= hi + 1e-9; v += step) out.push(Math.round(v * 1e6) / 1e6);
	return out;
}

export const clamp = (v: number, lo: number, hi: number) => Math.min(hi, Math.max(lo, v));
