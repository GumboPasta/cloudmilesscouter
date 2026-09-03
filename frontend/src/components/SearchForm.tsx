import { useId, useState } from "react";
import type { FormEvent, ReactNode } from "react";
import type { Cabin, SearchParams } from "../utils/api.ts";
import { AIRPORTS, formatAirport } from "../utils/airports.ts";

interface SearchFormProps {
  /** Called with validated params when the form is submitted. */
  onSubmit: (params: SearchParams) => void;
  /** True while a search is in flight — disables the submit button. */
  pending: boolean;
}

const CABINS: { value: "" | Cabin; label: string }[] = [
  { value: "", label: "Any cabin" },
  { value: "economy", label: "Economy" },
  { value: "premium_economy", label: "Premium economy" },
  { value: "business", label: "Business" },
  { value: "first", label: "First" },
];

const CODE_RE = /^[A-Z]{3}$/;

/** Today in YYYY-MM-DD, for the date input's `min`. */
function today(): string {
  return new Date().toISOString().slice(0, 10);
}

/** Uppercase and clamp free text to a 3-letter airport code. */
function toCode(raw: string): string {
  return raw.toUpperCase().replace(/[^A-Z]/g, "").slice(0, 3);
}

type Errors = Partial<Record<"origin" | "destination" | "date", string>>;

function validate(origin: string, destination: string, date: string): Errors {
  const errors: Errors = {};
  if (!CODE_RE.test(origin)) {
    errors.origin = "Enter a 3-letter airport code.";
  }
  if (!CODE_RE.test(destination)) {
    errors.destination = "Enter a 3-letter airport code.";
  } else if (destination === origin) {
    errors.destination = "Destination must differ from origin.";
  }
  if (!date) {
    errors.date = "Pick a date.";
  } else if (date < today()) {
    errors.date = "Pick a date that isn't in the past.";
  }
  return errors;
}

export function SearchForm({ onSubmit, pending }: SearchFormProps) {
  const [origin, setOrigin] = useState("");
  const [destination, setDestination] = useState("");
  const [date, setDate] = useState("");
  const [cabin, setCabin] = useState<"" | Cabin>("");
  const [errors, setErrors] = useState<Errors>({});

  const listId = useId();

  function handleSubmit(e: FormEvent) {
    e.preventDefault();
    const found = validate(origin, destination, date);
    setErrors(found);
    if (Object.keys(found).length > 0) {
      return;
    }
    onSubmit({
      origin,
      destination,
      date,
      ...(cabin ? { cabin } : {}),
    });
  }

  return (
    <form onSubmit={handleSubmit} noValidate className="grid gap-4 sm:grid-cols-2">
      <datalist id={listId}>
        {AIRPORTS.map((a) => (
          <option key={a.code} value={a.code}>
            {formatAirport(a)}
          </option>
        ))}
      </datalist>

      <AirportField
        label="Origin"
        listId={listId}
        value={origin}
        onChange={(v) => setOrigin(toCode(v))}
        error={errors.origin}
      />
      <AirportField
        label="Destination"
        listId={listId}
        value={destination}
        onChange={(v) => setDestination(toCode(v))}
        error={errors.destination}
      />

      <Field label="Date" error={errors.date}>
        {(id, describedBy) => (
          <input
            id={id}
            type="date"
            min={today()}
            value={date}
            aria-describedby={describedBy}
            onChange={(e) => setDate(e.target.value)}
            className="w-full rounded-md border border-slate-300 bg-white px-3 py-2 text-sm focus:border-sky-500 focus:outline-none focus:ring-1 focus:ring-sky-500"
          />
        )}
      </Field>

      <Field label="Cabin">
        {(id) => (
          <select
            id={id}
            value={cabin}
            onChange={(e) => setCabin(e.target.value as "" | Cabin)}
            className="w-full rounded-md border border-slate-300 bg-white px-3 py-2 text-sm focus:border-sky-500 focus:outline-none focus:ring-1 focus:ring-sky-500"
          >
            {CABINS.map((c) => (
              <option key={c.value} value={c.value}>
                {c.label}
              </option>
            ))}
          </select>
        )}
      </Field>

      <div className="sm:col-span-2">
        <button
          type="submit"
          disabled={pending}
          className="rounded-md bg-sky-600 px-4 py-2 text-sm font-medium text-white hover:bg-sky-700 focus:outline-none focus:ring-2 focus:ring-sky-500 focus:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-60"
        >
          {pending ? "Searching…" : "Search awards"}
        </button>
      </div>
    </form>
  );
}

interface FieldProps {
  label: string;
  error?: string;
  children: (id: string, describedBy: string | undefined) => ReactNode;
}

/** A labeled form row with an optional error message wired up for a11y. */
function Field({ label, error, children }: FieldProps) {
  const id = useId();
  const errorId = `${id}-error`;
  return (
    <div>
      <label htmlFor={id} className="block text-sm font-medium text-slate-700">
        {label}
      </label>
      <div className="mt-1">{children(id, error ? errorId : undefined)}</div>
      {error && (
        <p id={errorId} role="alert" className="mt-1 text-sm text-red-600">
          {error}
        </p>
      )}
    </div>
  );
}

interface AirportFieldProps {
  label: string;
  listId: string;
  value: string;
  onChange: (value: string) => void;
  error?: string;
}

function AirportField({ label, listId, value, onChange, error }: AirportFieldProps) {
  return (
    <Field label={label} error={error}>
      {(id, describedBy) => (
        <input
          id={id}
          list={listId}
          value={value}
          onChange={(e) => onChange(e.target.value)}
          placeholder="e.g. SFO"
          autoComplete="off"
          aria-describedby={describedBy}
          className="w-full rounded-md border border-slate-300 bg-white px-3 py-2 text-sm uppercase placeholder:normal-case focus:border-sky-500 focus:outline-none focus:ring-1 focus:ring-sky-500"
        />
      )}
    </Field>
  );
}
