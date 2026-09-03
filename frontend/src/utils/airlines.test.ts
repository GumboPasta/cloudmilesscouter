import { describe, expect, it } from "vitest";
import { ALLIANCES, allianceOf } from "./airlines.ts";

describe("allianceOf", () => {
  it("maps a known airline code", () => {
    expect(allianceOf("united")).toBe("Star Alliance");
    expect(allianceOf("american")).toBe("oneworld");
  });

  it("returns undefined for an unmapped code", () => {
    expect(allianceOf("jetblue")).toBeUndefined();
  });
});

describe("ALLIANCES", () => {
  it("has no duplicates", () => {
    expect(ALLIANCES).toHaveLength(new Set(ALLIANCES).size);
  });
});
