import { HomePage } from "./pages/HomePage.tsx";

export default function App() {
  return (
    <div className="flex min-h-screen flex-col bg-slate-50 text-slate-900">
      <header className="border-b border-slate-200 bg-white">
        <div className="mx-auto max-w-4xl px-4 py-5 sm:px-6">
          <div className="flex items-center gap-2">
            <span className="flex h-7 w-7 items-center justify-center rounded-lg bg-sky-600 text-white">
              <svg
                aria-hidden="true"
                viewBox="0 0 24 24"
                className="h-4 w-4"
                fill="currentColor"
              >
                <path d="M2.5 19.5 21 12 2.5 4.5 2.5 10 15 12 2.5 14z" />
              </svg>
            </span>
            <span className="text-lg font-semibold tracking-tight">
              CloudMilesScouter
            </span>
          </div>
          <p className="mt-1.5 text-sm text-slate-600">
            The best award availability across major carriers — aggregated,
            normalized, searchable.
          </p>
        </div>
      </header>

      <div className="flex-1">
        <HomePage />
      </div>

      <footer className="border-t border-slate-200 bg-white">
        <div className="mx-auto max-w-4xl px-4 py-4 text-xs text-slate-500 sm:px-6">
          Personal, non-commercial project. Award data is scraped on demand and
          may be stale.
        </div>
      </footer>
    </div>
  );
}
