// Client for the Go REST API (see docs/api.md). Every response is JSON; non-2xx
// responses carry {"error": "<message>"}.

const DEFAULT_BASE_URL = "http://localhost:8080";

/** Base URL of the API, from VITE_API_BASE_URL, without a trailing slash. */
export const API_BASE_URL = (
  import.meta.env.VITE_API_BASE_URL || DEFAULT_BASE_URL
).replace(/\/+$/, "");

export type Cabin = "economy" | "premium_economy" | "business" | "first";

export interface Health {
  status: string;
}

export interface Airline {
  code: string;
  name: string;
}

export interface RouteSummary {
  origin: string;
  destination: string;
  award_count: number;
  last_scraped: string;
}

export interface AwardOption {
  airline_code: string;
  airline_name: string;
  cabin: Cabin;
  flight_number: string;
  flight_origin: string;
  flight_destination: string;
  depart_time: string;
  arrive_time: string;
  duration_minutes: number;
  stops: number;
  award_type: string;
  points_cost: number;
  taxes_fees: number;
  currency: string;
  scraped_at: string;
}

export interface SearchParams {
  origin: string;
  destination: string;
  date: string;
  cabin?: Cabin;
}

export interface ScrapeRequest {
  origin: string;
  destination: string;
  date: string;
  airlines?: string[];
}

export interface ScrapeResponse {
  dispatched: string[];
  origin: string;
  destination: string;
  date: string;
}

/** Thrown for any non-2xx response, or a transport/parse failure. */
export class ApiError extends Error {
  readonly status: number;

  constructor(message: string, status: number, options?: ErrorOptions) {
    super(message, options);
    this.name = "ApiError";
    this.status = status;
  }
}

async function apiFetch<T>(path: string, init?: RequestInit): Promise<T> {
  let res: Response;
  try {
    res = await fetch(`${API_BASE_URL}${path}`, {
      ...init,
      headers: {
        Accept: "application/json",
        ...(init?.body ? { "Content-Type": "application/json" } : {}),
        ...init?.headers,
      },
    });
  } catch (cause) {
    throw new ApiError(`could not reach the API at ${API_BASE_URL}`, 0, {
      cause,
    });
  }

  const body = await res.text();
  const data = body ? (JSON.parse(body) as unknown) : null;

  if (!res.ok) {
    const message =
      data && typeof data === "object" && "error" in data
        ? String((data as { error: unknown }).error)
        : `request to ${path} failed with ${res.status}`;
    throw new ApiError(message, res.status);
  }

  return data as T;
}

export function getHealth(): Promise<Health> {
  return apiFetch<Health>("/healthz");
}

export function getAirlines(): Promise<Airline[]> {
  return apiFetch<Airline[]>("/api/airlines");
}

export function getRoutes(): Promise<RouteSummary[]> {
  return apiFetch<RouteSummary[]>("/api/routes");
}

export function searchAwards(params: SearchParams): Promise<AwardOption[]> {
  const qs = new URLSearchParams({
    origin: params.origin,
    destination: params.destination,
    date: params.date,
  });
  if (params.cabin) {
    qs.set("cabin", params.cabin);
  }
  return apiFetch<AwardOption[]>(`/api/search?${qs.toString()}`);
}

export function postScrape(body: ScrapeRequest): Promise<ScrapeResponse> {
  return apiFetch<ScrapeResponse>("/api/scrape", {
    method: "POST",
    body: JSON.stringify(body),
  });
}
