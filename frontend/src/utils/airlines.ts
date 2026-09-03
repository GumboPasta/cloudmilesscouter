// Static airline reference for the results table's alliance filter. The API
// exposes no alliance field (see docs/api.md — /api/search rows carry only
// airline_code / airline_name), and the scraper covers a fixed four carriers, so
// a bundled map is the pragmatic source. Add a line here when a new airline is
// scraped.

export const ALLIANCE_BY_CODE: Record<string, string> = {
  united: "Star Alliance",
  american: "oneworld",
  alaska: "oneworld",
  delta: "SkyTeam",
};

/** The distinct alliances present in ALLIANCE_BY_CODE, for a filter dropdown. */
export const ALLIANCES: string[] = [...new Set(Object.values(ALLIANCE_BY_CODE))];

/** Alliance for an airline code, or undefined if we don't have it mapped. */
export function allianceOf(code: string): string | undefined {
  return ALLIANCE_BY_CODE[code];
}
