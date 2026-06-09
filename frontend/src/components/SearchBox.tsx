import { useState } from "react";

// SearchBox is the query input + "Search ComicVine" CTA. It owns the input value and
// calls onSearch with the trimmed query on submit.
export function SearchBox({
  onSearch,
  pending,
}: {
  onSearch: (query: string) => void;
  pending: boolean;
}) {
  const [value, setValue] = useState("");

  return (
    <form
      className="flex gap-2"
      onSubmit={(e) => {
        e.preventDefault();
        const q = value.trim();
        if (q) {
          onSearch(q);
        }
      }}
    >
      <input
        type="text"
        value={value}
        onChange={(e) => setValue(e.target.value)}
        placeholder="Search ComicVine for a series…"
        className="flex-1 rounded bg-slate-100 px-4 py-2 text-base focus:outline-none focus:ring-2 focus:ring-blue-600"
      />
      <button
        type="submit"
        disabled={pending}
        className="rounded bg-blue-600 px-4 py-2 text-sm font-semibold text-white disabled:opacity-50"
      >
        Search ComicVine
      </button>
    </form>
  );
}
