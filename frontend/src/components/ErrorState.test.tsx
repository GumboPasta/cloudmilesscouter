import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ErrorState } from "./ErrorState.tsx";

afterEach(cleanup);

describe("ErrorState", () => {
  it("shows the message", () => {
    render(<ErrorState message="invalid cabin" onRetry={vi.fn()} />);
    expect(screen.getByRole("alert")).toHaveTextContent("invalid cabin");
  });

  it("calls onRetry when the button is clicked", () => {
    const onRetry = vi.fn();
    render(<ErrorState message="boom" onRetry={onRetry} />);
    fireEvent.click(screen.getByRole("button", { name: /try again/i }));
    expect(onRetry).toHaveBeenCalledTimes(1);
  });
});
