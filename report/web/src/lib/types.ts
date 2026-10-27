// The shape of data/report.json, written by `birdnet-snapshot report`.
export interface Photo {
	large: string;
	small: string;
	author?: string;
	license?: string;
	licenseUrl?: string;
	source: string;
}

export interface Species {
	code: string;
	common: string;
	scientific: string;
	family?: string;
	order?: string;
	count: number;
	share: number;
	avgConfidence: number;
	maxConfidence: number;
	firstHeard: string;
	lastHeard: string;
	daysHeard: number;
	hourly: number[];
	daily: number[];
	peakHour: number;
	nightShare: number;
	rarity?: string;
	rarityScore?: number;
	isNew: boolean;
	photo?: Photo;
	/** Detections in the period before, and the change per listening day (0.24 = up 24%), when there is one to compare with. */
	prev?: number;
	change?: number | null;
	/** Best recordings, for the species' own view. */
	clips?: Recording[];
}

export interface Recording {
	common: string;
	scientific: string;
	code: string;
	confidence: number;
	time: string;
	models: number;
	audio: string;
	spectrogram?: string;
}

export interface Report {
	version: string;
	generatedAt: string;
	title: string;
	subtitle: string;
	station: string;
	period: { start: string; end: string; days: number; label: string };
	pdf?: string;
	totals: {
		detections: number;
		species: number;
		newSpecies: number;
		activeDays: number;
		busiestHour: number;
		busiestDay: string;
		busiestDayCount: number;
		nightShare: number;
	};
	hourly: number[];
	daily: { date: string; detections: number; species: number }[];
	species: Species[];
	highlights: string[];
	recordings?: { best: Recording[]; recent: Recording[] };
	dawn?: { series: OnsetSeries[] };
	compare?: Compare;
	quality?: Quality;
	growth?: Growth;
	weather?: Weather;
	soundscape?: Soundscape;
}

/** One morning singer's dawn-chorus onset, a point per day of the period. */
export interface OnsetSeries {
	code: string;
	common: string;
	days: { date: string; count: number; minutes: number | null }[];
}

export interface Compare {
	label: string;
	start: string;
	end: string;
	detections: number;
	species: number;
	activeDays: number;
	perDay: number;
	prevPerDay: number;
	change?: number;
	notHeard?: { code: string; common: string; count: number }[];
	quiet?: { code: string; common: string; prev: number; now: number; change: number }[];
}

export interface Quality {
	kept: number;
	removed: number;
	twoModel: number;
	high: number;
	median: number;
	/** Detections per 0.1 of confidence, from 0.2 to 1.0. */
	histogram: number[];
	cautions?: { code: string; common: string; count: number; mean: number; twoModel: number; reason: string }[];
}

export interface Growth {
	days: { date: string; new: number; total: number; species?: string[] }[];
	total: number;
}

export interface WeatherHour {
	n: number;
	t?: number;
	w?: number;
	c?: number;
	o?: string;
	r?: boolean;
}

export interface Weather {
	start: string;
	hours: WeatherHour[];
	insight?: string;
}

export interface Soundscape {
	bands: string[];
	levels: (number | null)[][];
	min: number;
	max: number;
	quietest: number;
	loudest: number;
	insight?: string;
}
