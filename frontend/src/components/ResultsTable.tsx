import { useId, useMemo, useState } from "react";
import type { ReactNode } from "react";
import type { AwardOption, Cabin } from "../utils/api.ts";
import { allianceOf } from "../utils/airlines.ts";

interface ResultsTableProps {
  /** Award options as returned by GET /api/search (already points-ascending). */
  results: AwardOption[];
}

const CABIN_LABEL: Record<Cabin, string> = {
  economy: "Economy",
  premium_economy: "Premium economy",
  business: "Business",
  first: "First",
};

const CABIN_RANK: Record<Cabin, number> = {
  economy: 0,
  premium_economy: 1,
  business: 2,
  first: 3,
};

type SortKey = "airline" | "cabin" | "duration" | "points";

interface Sort {
  key: SortKey;
  dir: "asc" | "desc";
}

const SELECT_CLASS =
  "mt-1 block rounded-md border border-slate-300 bg-white px-3 py-2 text-sm focus:border-sky-500 focus:outline-none focus:ring-1 focus:ring-sky-500";

interface FilterSelectProps {
  label: string;
  value: string;
  onChange: (value: string) => void;
  /** The non-"All" <option> elements. */
  children: ReactNode;
}

/** A labeled filter dropdown with a leading "All" option (value ""). */
function FilterSelect({ label, value, onChange, children }: FilterSelectProps) {
  const id = useId();
  return (
    <div>
      <label htmlFor={id} className="block text-sm font-medium text-slate-700">
        {label}
      </label>
      <select
        id={id}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className={SELECT_CLASS}
      >
        <option value="">All</option>
        {children}
      </select>
    </div>
  );
}

interface SortHeaderProps {
  col: SortableColumn;
  sort: Sort;
  onSort: (key: SortKey) => void;
}

/** A column header that toggles the table's sort when clicked. */
function SortHeader({ col, sort, onSort }: SortHeaderProps) {
  const active = sort.key === col.key;
  return (
    <th
      scope="col"
      aria-sort={
        active ? (sort.dir === "asc" ? "ascending" : "descending") : "none"
      }
      className={`py-2 pr-4 font-medium ${col.align === "right" ? "text-right" : ""}`}
    >
      <button
        type="button"
        onClick={() => onSort(col.key)}
        className="inline-flex items-center gap-1 hover:text-slate-900"
      >
        {col.label}
        <span aria-hidden="true" className="text-xs">
          {active ? (sort.dir === "asc" ? "▲" : "▼") : ""}
        </span>
      </button>
    </th>
  );
}

function formatDuration(minutes: number): string {
  const h = Math.floor(minutes / 60);
  const m = minutes % 60;
  return `${h}h ${m}m`;
}

function formatTime(iso: string): string {
  const d = new Date(iso);
  return Number.isNaN(d.getTime())
    ? iso
    : d.toLocaleTimeString([], { hour: "numeric", minute: "2-digit" });
}

function formatStops(stops: number): string {
  if (stops <= 0) return "Nonstop";
  return `${stops} stop${stops === 1 ? "" : "s"}`;
}

function formatMoney(amount: number, currency: string): string {
  try {
    return amount.toLocaleString(undefined, { style: "currency", currency });
  } catch {
    return `${amount.toLocaleString()} ${currency}`;
  }
}

function compare(a: AwardOption, b: AwardOption, key: SortKey): number {
  switch (key) {
    case "airline":
      return a.airline_name.localeCompare(b.airline_name);
    case "cabin":
      return CABIN_RANK[a.cabin] - CABIN_RANK[b.cabin];
    case "duration":
      return a.duration_minutes - b.duration_minutes;
    case "points":
      return a.points_cost - b.points_cost;
  }
}

interface SortableColumn {
  key: SortKey;
  label: string;
  align?: "right";
}

const SORTABLE_COLUMNS: SortableColumn[] = [
  { key: "airline", label: "Airline" },
  { key: "cabin", label: "Cabin" },
  { key: "duration", label: "Duration" },
  { key: "points", label: "Points", align: "right" },
];

export function ResultsTable({ results }: ResultsTableProps) {
  const [airline, setAirline] = useState("");
  const [cabin, setCabin] = useState("");
  const [alliance, setAlliance] = useState("");
  const [sort, setSort] = useState<Sort>({ key: "points", dir: "asc" });

  const airlineOptions = useMemo(() => {
    const seen = new Map<string, string>();
    for (const r of results) seen.set(r.airline_code, r.airline_name);
    return [...seen].sort((a, b) => a[1].localeCompare(b[1]));
  }, [results]);

  const cabinOptions = useMemo(
    () =>
      [...new Set(results.map((r) => r.cabin))].sort(
        (a, b) => CABIN_RANK[a] - CABIN_RANK[b],
      ),
    [results],
  );

  const allianceOptions = useMemo(() => {
    const present = results
      .map((r) => allianceOf(r.airline_code))
      .filter((a): a is string => a !== undefined);
    return [...new Set(present)].sort();
  }, [results]);

  const rows = useMemo(() => {
    const filtered = results.filter(
      (r) =>
        (!airline || r.airline_code === airline) &&
        (!cabin || r.cabin === cabin) &&
        (!alliance || allianceOf(r.airline_code) === alliance),
    );
    const factor = sort.dir === "asc" ? 1 : -1;
    return filtered.sort((a, b) => {
      const primary = compare(a, b, sort.key) * factor;
      return primary !== 0 ? primary : a.points_cost - b.points_cost;
    });
  }, [results, airline, cabin, alliance, sort]);

  function toggleSort(key: SortKey) {
    setSort((prev) =>
      prev.key === key
        ? { key, dir: prev.dir === "asc" ? "desc" : "asc" }
        : { key, dir: "asc" },
    );
  }

  return (
    <div>
      <div className="flex flex-wrap items-end gap-2 sm:gap-3">
        <FilterSelect label="Airline" value={airline} onChange={setAirline}>
          {airlineOptions.map(([code, name]) => (
            <option key={code} value={code}>
              {name}
            </option>
          ))}
        </FilterSelect>

        <FilterSelect label="Cabin" value={cabin} onChange={setCabin}>
          {cabinOptions.map((c) => (
            <option key={c} value={c}>
              {CABIN_LABEL[c]}
            </option>
          ))}
        </FilterSelect>

        <FilterSelect label="Alliance" value={alliance} onChange={setAlliance}>
          {allianceOptions.map((a) => (
            <option key={a} value={a}>
              {a}
            </option>
          ))}
        </FilterSelect>
      </div>

      <p className="mt-3 text-sm text-slate-600">
        {rows.length} of {results.length} shown
      </p>

      {rows.length === 0 ? (
        <p className="mt-4 text-sm text-slate-500">
          No results match these filters.
        </p>
      ) : (
        <div className="mt-2 overflow-x-auto">
          <table className="w-full min-w-[44rem] border-collapse whitespace-nowrap text-sm">
            <caption className="sr-only">
              Award options, sorted by {sort.key} {sort.dir}
            </caption>
            <thead>
              <tr className="border-b border-slate-300 text-left text-slate-600">
                <SortHeader col={SORTABLE_COLUMNS[0]} sort={sort} onSort={toggleSort} />
                <th scope="col" className="py-2 pr-4 font-medium">
                  Route
                </th>
                <SortHeader col={SORTABLE_COLUMNS[1]} sort={sort} onSort={toggleSort} />
                <SortHeader col={SORTABLE_COLUMNS[2]} sort={sort} onSort={toggleSort} />
                <SortHeader col={SORTABLE_COLUMNS[3]} sort={sort} onSort={toggleSort} />
                <th scope="col" className="py-2 pr-4 font-medium">
                  Type
                </th>
                <th scope="col" className="py-2 text-right font-medium">
                  Taxes
                </th>
              </tr>
            </thead>
            <tbody>
              {rows.map((r, i) => (
                <tr
                  key={`${r.airline_code}-${r.flight_number}-${r.cabin}-${i}`}
                  className="border-b border-slate-100"
                >
                  <td className="py-2 pr-4">{r.airline_name}</td>
                  <td className="py-2 pr-4">
                    <span className="tabular-nums">
                      {r.flight_origin}→{r.flight_destination}
                    </span>
                    <span className="ml-2 text-slate-500">
                      {formatStops(r.stops)}
                    </span>
                    <span className="ml-2 text-slate-400">
                      dep {formatTime(r.depart_time)}
                    </span>
                  </td>
                  <td className="py-2 pr-4">{CABIN_LABEL[r.cabin]}</td>
                  <td className="py-2 pr-4 tabular-nums">
                    {formatDuration(r.duration_minutes)}
                  </td>
                  <td className="py-2 pr-4 text-right tabular-nums">
                    {r.points_cost.toLocaleString()}
                  </td>
                  <td className="py-2 pr-4 text-slate-600">{r.award_type}</td>
                  <td className="py-2 text-right tabular-nums text-slate-600">
                    {formatMoney(r.taxes_fees, r.currency)}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
