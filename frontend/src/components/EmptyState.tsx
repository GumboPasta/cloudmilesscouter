import type { SearchParams } from "../utils/api.ts";

interface EmptyStateProps {
  params: SearchParams;
}

/** Shown when the search succeeded but no awards exist for the route and date. */
export function EmptyState({ params }: EmptyStateProps) {
  return (
    <div className="rounded-lg border border-slate-200 bg-white p-8 text-center">
      <svg
        aria-hidden="true"
        viewBox="0 0 24 24"
        className="mx-auto h-9 w-9 text-slate-400"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.6"
        strokeLinecap="round"
        strokeLinejoin="round"
      >
        <path d="M2 12h5l2 3h6l2-3h5" />
        <path d="M4 12l2-6h12l2 6" />
      </svg>
      <p className="mt-3 text-sm font-medium text-slate-800">
        No award options found
      </p>
      <p className="mt-1 text-sm text-slate-500">
        Nothing on file for {params.origin} → {params.destination} on{" "}
        {params.date}
        {params.cabin ? ` in ${params.cabin.replace("_", " ")}` : ""}. A scrape
        may still be running — try again in a minute.
      </p>
    </div>
  );
}
