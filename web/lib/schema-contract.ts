import { existsSync } from "node:fs";
import path from "node:path";

export const CANONICAL_SCHEMA_SQL_RELATIVE_PATH = "../schema/schema.sql";
export const CANONICAL_SCHEMA_TYPES_RELATIVE_PATH =
  "../schema/schema-types.ts";

export type CanonicalSchemaPaths = {
  schemaSqlPath: string;
  schemaTypesPath: string;
};

/**
 * Resolves canonical root schema file paths for the web workspace.
 *
 * @param webRoot - Absolute path to the web workspace root.
 * @returns Absolute schema file paths derived from canonical relative paths.
 */
export function getCanonicalSchemaPaths(
  webRoot: string = process.cwd()
): CanonicalSchemaPaths {
  return {
    schemaSqlPath: path.resolve(webRoot, CANONICAL_SCHEMA_SQL_RELATIVE_PATH),
    schemaTypesPath: path.resolve(
      webRoot,
      CANONICAL_SCHEMA_TYPES_RELATIVE_PATH
    )
  };
}

/**
 * Verifies canonical schema artifacts exist at the required root paths.
 *
 * @param webRoot - Absolute path to the web workspace root.
 * @returns Resolved canonical schema paths when both files exist.
 * @throws Error if either canonical schema file is missing.
 */
export function assertCanonicalSchemaContract(
  webRoot: string = process.cwd()
): CanonicalSchemaPaths {
  const paths = getCanonicalSchemaPaths(webRoot);
  const missingPaths: string[] = [];

  if (!existsSync(paths.schemaSqlPath)) {
    missingPaths.push(CANONICAL_SCHEMA_SQL_RELATIVE_PATH);
  }

  if (!existsSync(paths.schemaTypesPath)) {
    missingPaths.push(CANONICAL_SCHEMA_TYPES_RELATIVE_PATH);
  }

  if (missingPaths.length > 0) {
    throw new Error(
      `Missing canonical schema file(s): ${missingPaths.join(", ")}`
    );
  }

  return paths;
}
