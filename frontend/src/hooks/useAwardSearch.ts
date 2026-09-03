import { useCallback, useEffect, useRef, useState } from "react";
import {
  ApiError,
  postScrape,
  searchAwards,
  type AwardOption,
  type SearchParams,
} from "../utils/api.ts";

export type SearchStatus = "idle" | "loading" | "success" | "error";

export interface AwardSearch {
  status: SearchStatus;
  results: AwardOption[];
  error: string | null;
  /**
   * Set when the scrape dispatch failed but the search still ran — the results
   * are whatever was already in the database. Non-blocking.
   */
  scrapeWarning: string | null;
  /** Fires a scrape (best-effort) then fetches award results for `params`. */
  run: (params: SearchParams) => void;
}

/**
 * Owns the async state for one award search. `run` triggers a fresh scrape via
 * POST /api/scrape — best-effort, a failure there surfaces as `scrapeWarning`
 * rather than an error since the worker pool may not be running — then reads
 * current results from GET /api/search.
 */
export function useAwardSearch(): AwardSearch {
  const [status, setStatus] = useState<SearchStatus>("idle");
  const [results, setResults] = useState<AwardOption[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [scrapeWarning, setScrapeWarning] = useState<string | null>(null);

  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  const run = useCallback((params: SearchParams) => {
    setStatus("loading");
    setError(null);
    setScrapeWarning(null);

    void (async () => {
      try {
        await postScrape(params);
      } catch (err) {
        console.warn("scrape dispatch failed, searching existing data", err);
        if (mounted.current) {
          setScrapeWarning(
            "Couldn't queue a fresh scrape — showing results already on file.",
          );
        }
      }

      try {
        const awards = await searchAwards(params);
        if (!mounted.current) return;
        setResults(awards);
        setStatus("success");
      } catch (err) {
        if (!mounted.current) return;
        setResults([]);
        setError(
          err instanceof ApiError ? err.message : "Something went wrong.",
        );
        setStatus("error");
      }
    })();
  }, []);

  return { status, results, error, scrapeWarning, run };
}
