import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { SearchForm } from "./SearchForm.tsx";

afterEach(cleanup);

const FUTURE = "2099-12-20";

function fill(values: { origin?: string; destination?: string; date?: string; cabin?: string }) {
  if (values.origin !== undefined) {
    fireEvent.change(screen.getByLabelText("Origin"), {
      target: { value: values.origin },
    });
  }
  if (values.destination !== undefined) {
    fireEvent.change(screen.getByLabelText("Destination"), {
      target: { value: values.destination },
    });
  }
  if (values.date !== undefined) {
    fireEvent.change(screen.getByLabelText("Date"), {
      target: { value: values.date },
    });
  }
  if (values.cabin !== undefined) {
    fireEvent.change(screen.getByLabelText("Cabin"), {
      target: { value: values.cabin },
    });
  }
}

function submit() {
  fireEvent.click(screen.getByRole("button", { name: /search awards/i }));
}

describe("SearchForm", () => {
  it("renders all fields and the submit button", () => {
    render(<SearchForm onSubmit={vi.fn()} pending={false} />);
    expect(screen.getByLabelText("Origin")).toBeInTheDocument();
    expect(screen.getByLabelText("Destination")).toBeInTheDocument();
    expect(screen.getByLabelText("Date")).toBeInTheDocument();
    expect(screen.getByLabelText("Cabin")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /search awards/i })).toBeEnabled();
  });

  it("blocks submit and shows errors when empty", () => {
    const onSubmit = vi.fn();
    render(<SearchForm onSubmit={onSubmit} pending={false} />);
    submit();
    expect(onSubmit).not.toHaveBeenCalled();
    expect(screen.getAllByText(/3-letter airport code/i)).toHaveLength(2);
    expect(screen.getByText(/pick a date/i)).toBeInTheDocument();
  });

  it("rejects a destination equal to the origin", () => {
    const onSubmit = vi.fn();
    render(<SearchForm onSubmit={onSubmit} pending={false} />);
    fill({ origin: "sfo", destination: "sfo", date: FUTURE });
    submit();
    expect(onSubmit).not.toHaveBeenCalled();
    expect(screen.getByText(/must differ from origin/i)).toBeInTheDocument();
  });

  it("rejects a past date", () => {
    const onSubmit = vi.fn();
    render(<SearchForm onSubmit={onSubmit} pending={false} />);
    fill({ origin: "SFO", destination: "JFK", date: "2000-01-01" });
    submit();
    expect(onSubmit).not.toHaveBeenCalled();
    expect(screen.getByText(/isn't in the past/i)).toBeInTheDocument();
  });

  it("uppercases typed airport codes", () => {
    render(<SearchForm onSubmit={vi.fn()} pending={false} />);
    fill({ origin: "sfo" });
    expect(screen.getByLabelText("Origin")).toHaveValue("SFO");
  });

  it("submits validated params and omits an unset cabin", () => {
    const onSubmit = vi.fn();
    render(<SearchForm onSubmit={onSubmit} pending={false} />);
    fill({ origin: "SFO", destination: "JFK", date: FUTURE });
    submit();
    expect(onSubmit).toHaveBeenCalledWith({
      origin: "SFO",
      destination: "JFK",
      date: FUTURE,
    });
  });

  it("includes the cabin when one is chosen", () => {
    const onSubmit = vi.fn();
    render(<SearchForm onSubmit={onSubmit} pending={false} />);
    fill({ origin: "SFO", destination: "JFK", date: FUTURE, cabin: "business" });
    submit();
    expect(onSubmit).toHaveBeenCalledWith({
      origin: "SFO",
      destination: "JFK",
      date: FUTURE,
      cabin: "business",
    });
  });

  it("disables and relabels the button while pending", () => {
    render(<SearchForm onSubmit={vi.fn()} pending={true} />);
    expect(screen.getByRole("button", { name: /searching/i })).toBeDisabled();
  });
});
