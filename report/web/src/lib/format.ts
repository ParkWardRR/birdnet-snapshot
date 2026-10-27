export const num = (n: number) => n.toLocaleString('en-US');

export const pct = (f: number) => `${Math.round(f * 100)}%`;

export function hour(h: number): string {
	if (h === 0) return 'midnight';
	if (h === 12) return 'noon';
	return h < 12 ? `${h} am` : `${h - 12} pm`;
}

export const shortHour = (h: number) => (h === 0 ? '12a' : h === 12 ? '12p' : h < 12 ? `${h}a` : `${h - 12}p`);

// Dates arrive as YYYY-MM-DD in the station's zone; format them without
// letting the viewer's zone shift the day.
export function day(d: string, opts: Intl.DateTimeFormatOptions = { weekday: 'short', month: 'short', day: 'numeric' }) {
	const [y, m, dd] = d.split('-').map(Number);
	return new Date(Date.UTC(y, m - 1, dd)).toLocaleDateString('en-US', { ...opts, timeZone: 'UTC' });
}

// Timestamps carry the station's offset; show them in that zone's wall time.
export function stamp(iso: string): string {
	const m = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})/.exec(iso);
	if (!m) return iso;
	const [, y, mo, d, h, mi] = m;
	const date = day(`${y}-${mo}-${d}`, { month: 'short', day: 'numeric' });
	const hh = Number(h);
	const ampm = hh === 0 ? `12:${mi} am` : hh < 12 ? `${hh}:${mi} am` : hh === 12 ? `12:${mi} pm` : `${hh - 12}:${mi} pm`;
	return `${date}, ${ampm}`;
}

export const isNight = (h: number) => h >= 19 || h < 6;

/** "7 am – 8 am", "11 pm – midnight": the hour that starts at h. */
export const hourSpan = (h: number) => `${hour(h)} – ${hour((h + 1) % 24)}`;

/** Signed minutes: "+30 min", "−12 min", "at dawn". */
export function offset(min: number): string {
	if (min === 0) return 'at dawn';
	return `${min > 0 ? '+' : '−'}${Math.abs(min)} min`;
}
