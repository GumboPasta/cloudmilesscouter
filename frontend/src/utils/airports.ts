// A small static list of well-known airports, used only to populate the
// autocomplete <datalist> on the search form. The API has no airport endpoint,
// and /api/routes returns metro strings rather than clean IATA codes, so this
// bundled list is the pragmatic source. It does not need to be exhaustive — the
// airport inputs accept any 3-letter code, this just speeds up the common ones.

export interface Airport {
  code: string;
  name: string;
  city: string;
}

/** Label shown for an airport in the autocomplete list. */
export function formatAirport(a: Airport): string {
  return `${a.code} — ${a.city} (${a.name})`;
}

export const AIRPORTS: Airport[] = [
  { code: "ATL", name: "Hartsfield–Jackson", city: "Atlanta" },
  { code: "AUS", name: "Austin–Bergstrom", city: "Austin" },
  { code: "BNA", name: "Nashville International", city: "Nashville" },
  { code: "BOS", name: "Logan International", city: "Boston" },
  { code: "BWI", name: "Baltimore/Washington", city: "Baltimore" },
  { code: "CLT", name: "Charlotte Douglas", city: "Charlotte" },
  { code: "DCA", name: "Reagan National", city: "Washington" },
  { code: "DEN", name: "Denver International", city: "Denver" },
  { code: "DFW", name: "Dallas/Fort Worth", city: "Dallas" },
  { code: "DTW", name: "Detroit Metropolitan", city: "Detroit" },
  { code: "EWR", name: "Newark Liberty", city: "Newark" },
  { code: "FLL", name: "Fort Lauderdale–Hollywood", city: "Fort Lauderdale" },
  { code: "HNL", name: "Daniel K. Inouye", city: "Honolulu" },
  { code: "IAD", name: "Washington Dulles", city: "Washington" },
  { code: "IAH", name: "George Bush Intercontinental", city: "Houston" },
  { code: "JFK", name: "John F. Kennedy", city: "New York" },
  { code: "LAS", name: "Harry Reid International", city: "Las Vegas" },
  { code: "LAX", name: "Los Angeles International", city: "Los Angeles" },
  { code: "LGA", name: "LaGuardia", city: "New York" },
  { code: "MCO", name: "Orlando International", city: "Orlando" },
  { code: "MIA", name: "Miami International", city: "Miami" },
  { code: "MSP", name: "Minneapolis–Saint Paul", city: "Minneapolis" },
  { code: "ORD", name: "O'Hare International", city: "Chicago" },
  { code: "PDX", name: "Portland International", city: "Portland" },
  { code: "PHL", name: "Philadelphia International", city: "Philadelphia" },
  { code: "PHX", name: "Phoenix Sky Harbor", city: "Phoenix" },
  { code: "RDU", name: "Raleigh–Durham", city: "Raleigh" },
  { code: "SAN", name: "San Diego International", city: "San Diego" },
  { code: "SEA", name: "Seattle–Tacoma", city: "Seattle" },
  { code: "SFO", name: "San Francisco International", city: "San Francisco" },
  { code: "SJC", name: "Norman Y. Mineta San José", city: "San Jose" },
  { code: "SLC", name: "Salt Lake City International", city: "Salt Lake City" },
  { code: "TPA", name: "Tampa International", city: "Tampa" },
  { code: "ANC", name: "Ted Stevens Anchorage", city: "Anchorage" },
  { code: "YVR", name: "Vancouver International", city: "Vancouver" },
  { code: "YYZ", name: "Toronto Pearson", city: "Toronto" },
  { code: "LHR", name: "Heathrow", city: "London" },
  { code: "CDG", name: "Charles de Gaulle", city: "Paris" },
  { code: "FRA", name: "Frankfurt am Main", city: "Frankfurt" },
  { code: "AMS", name: "Schiphol", city: "Amsterdam" },
  { code: "NRT", name: "Narita International", city: "Tokyo" },
  { code: "HND", name: "Haneda", city: "Tokyo" },
  { code: "ICN", name: "Incheon International", city: "Seoul" },
  { code: "SIN", name: "Changi", city: "Singapore" },
  { code: "HKG", name: "Hong Kong International", city: "Hong Kong" },
  { code: "SYD", name: "Kingsford Smith", city: "Sydney" },
  { code: "GRU", name: "Guarulhos", city: "São Paulo" },
  { code: "MEX", name: "Benito Juárez", city: "Mexico City" },
];
