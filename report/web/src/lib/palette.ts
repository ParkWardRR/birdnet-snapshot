// Categorical slots for the charts that tell species apart. Checked with the
// dataviz palette validator against the card surface (#fbf8f1, light mode):
// slots 1–5 in this order clear the lightness band and chroma floor, adjacent
// colour-blind ΔE is 14.2 and normal-vision ΔE 25.7. Gold is 2.68:1 on the
// card (under the 3:1 mark contrast), which is why every chart here also has
// labels and a table view. Overlaid lines would fail the all-pairs check
// (indigo/violet), so the dawn chart is small multiples instead.
//
// A species takes the slot of its rank by detections, and wears it in every
// chart of one report.
export const SERIES = ['#2f8a4f', '#8e4fa0', '#c78f1c', '#4a5fa8', '#c0612b'];

/** How many species the hour-of-day charts draw. */
export const CAST = SERIES.length;

export const colorAt = (i: number) => SERIES[i] ?? '#6a7266';

/** White or ink text for a label set on a fill of this colour. */
export function inkOn(hex: string): string {
	const [r, g, b] = [1, 3, 5].map((i) => {
		const c = parseInt(hex.slice(i, i + 2), 16) / 255;
		return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
	});
	return 0.2126 * r + 0.7152 * g + 0.0722 * b > 0.3 ? '#1f2a24' : '#fbf8f1';
}
