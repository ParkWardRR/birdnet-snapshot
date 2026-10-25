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
}
