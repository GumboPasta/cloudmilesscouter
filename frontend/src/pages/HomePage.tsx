import { useState } from "react";
import { ApiStatusBadge } from "../components/ApiStatusBadge.tsx";
import { EmptyState } from "../components/EmptyState.tsx";
import { ErrorState } from "../components/ErrorState.tsx";
import { ResultsTable } from "../components/ResultsTable.tsx";
import { SearchForm } from "../components/SearchForm.tsx";
import { useAwardSearch } from "../hooks/useAwardSearch.ts";
import type { SearchParams } from "../utils/api.ts";

/**
 * Landing page: the award search form wired to a scrape + search (Step 2),
 * the sortable/filterable results table (Step 3), and the loading / empty /
 * error states (Step 4).
 */
export function HomePage() {
  const { status, results, error, scrapeWarning, run } = useAwardSearch();
  const [submitted, setSubmitted] = useState<SearchParams | null>(null);

  function handleSubmit(params: SearchParams) {
    setSubmitted(params);
    run(params);
  }

  function retry() {
    if (submitted) run(submitted);
  }

  return (
    <main className="mx-auto max-w-4xl px-4 py-10 sm:px-6 sm:py-16">
      <div className="flex justify-end">
        <ApiStatusBadge />
      </div>

      <div className="mt-6">
        <SearchForm onSubmit={handleSubmit} pending={status === "loading"} />
      </div>

      <div className="mt-10">
        {status === "loading" && (
          <div className="flex items-center gap-3 text-sm text-slate-500">
            <span className="h-4 w-4 animate-spin rounded-full border-2 border-slate-300 border-t-sky-600" />
            Searching…
          </div>
        )}

        {status === "error" && error && (
          <ErrorState message={error} onRetry={retry} />
        )}

        {status === "success" &&
          (results.length === 0 && submitted ? (
            <EmptyState params={submitted} />
          ) : (
            <div>
              {scrapeWarning && (
                <p className="mb-4 rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-sm text-amber-800">
                  {scrapeWarning}
                </p>
              )}
              <ResultsTable results={results} />
            </div>
          ))}
      </div>
    </main>
  );
}
