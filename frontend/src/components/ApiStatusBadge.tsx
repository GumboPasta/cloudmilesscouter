import { useApiHealth } from "../hooks/useApiHealth.ts";
import { API_BASE_URL } from "../utils/api.ts";

const LABELS = {
  loading: "Checking API…",
  ok: "API reachable",
  error: "API unreachable",
} as const;

const DOT = {
  loading: "bg-amber-400",
  ok: "bg-emerald-500",
  error: "bg-red-500",
} as const;

/** Small indicator that the frontend can talk to the Go API at API_BASE_URL. */
export function ApiStatusBadge() {
  const health = useApiHealth();

  return (
    <span
      className="inline-flex items-center gap-2 rounded-full border border-slate-200 bg-white px-3 py-1 text-sm text-slate-600"
      title={API_BASE_URL}
    >
      <span className={`h-2 w-2 rounded-full ${DOT[health]}`} />
      {LABELS[health]}
    </span>
  );
}
