import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { afterEach, describe, expect, it } from "vitest";
import {
  assertCanonicalSchemaContract,
  getCanonicalSchemaPaths
} from "../../lib/schema-contract";

const tempDirectories: string[] = [];

afterEach(() => {
  for (const directoryPath of tempDirectories) {
    rmSync(directoryPath, { recursive: true, force: true });
  }
  tempDirectories.length = 0;
});

describe("schema contract", () => {
  it("resolves canonical schema paths from the web workspace root", () => {
    const resolvedPaths = getCanonicalSchemaPaths(process.cwd());

    expect(resolvedPaths).toEqual({
      schemaSqlPath: path.resolve(process.cwd(), "../schema/schema.sql"),
      schemaTypesPath: path.resolve(
        process.cwd(),
        "../schema/schema-types.ts"
      )
    });
  });

  it("passes when canonical schema files are present at root paths", () => {
    const resolvedPaths = assertCanonicalSchemaContract(process.cwd());

    expect(resolvedPaths.schemaSqlPath.endsWith("schema/schema.sql")).toBe(true);
    expect(
      resolvedPaths.schemaTypesPath.endsWith("schema/schema-types.ts")
    ).toBe(true);
  });

  it("fails loudly when canonical root schema files are missing", () => {
    const tempRootPath = mkdtempSync(path.join(tmpdir(), "pc-web-schema-"));
    tempDirectories.push(tempRootPath);

    expect(() => assertCanonicalSchemaContract(tempRootPath)).toThrowError(
      /Missing canonical schema file\(s\): \.\.\/schema\/schema\.sql, \.\.\/schema\/schema-types\.ts/
    );
  });
});
