import assert from "node:assert/strict";
import { readdir, readFile } from "node:fs/promises";
import path from "node:path";
import test from "node:test";

const root = path.resolve(import.meta.dirname);
const readJson = async (relativePath) => JSON.parse(await readFile(path.join(root, relativePath), "utf8"));
const schema = await readJson("schema/exercise.schema.json");
const exerciseFiles = (await readdir(path.join(root, "exercises"))).filter((file) => file.endsWith(".json")).sort();
const sections = await Promise.all(exerciseFiles.map((file) => readJson(`exercises/${file}`)));
const exercises = sections.flatMap((section) => section.exercises);
const dictionaries = {
  equipment: new Set((await readJson("dictionaries/equipment.json")).items.map((item) => item.code)),
  targets: new Set((await readJson("dictionaries/load-targets.json")).items.map((item) => item.code)),
  muscles: new Set((await readJson("dictionaries/muscle-groups.json")).items.map((item) => item.code)),
  profiles: new Set((await readJson("dictionaries/load-profiles.json")).items.map((item) => item.code)),
  levels: new Set((await readJson("dictionaries/levels.json")).items.map((item) => item.code)),
  patterns: new Set((await readJson("dictionaries/movement-patterns.json")).items.map((item) => item.code)),
  sports: new Set((await readJson("dictionaries/sports.json")).items.map((item) => item.code)),
  sections: new Set((await readJson("dictionaries/sections.json")).items.map((item) => item.code)),
};

function schemaTypes(value) {
  if (value === null) return "null";
  if (Array.isArray(value)) return "array";
  if (Number.isInteger(value)) return "integer";
  return typeof value;
}

function assertSchemaValue(value, rule, at) {
  if ("const" in rule) assert.deepEqual(value, rule.const, `${at} must equal its schema constant`);
  if (rule.enum) assert.ok(rule.enum.some((candidate) => Object.is(candidate, value)), `${at} must be an allowed enum value`);
  if (rule.type) {
    const expected = Array.isArray(rule.type) ? rule.type : [rule.type];
    assert.ok(expected.includes(schemaTypes(value)), `${at} must be ${expected.join(" or ")}`);
  }
  if (value === null || value === undefined) return;
  if (typeof value === "string") {
    if (rule.minLength !== undefined) assert.ok(value.length >= rule.minLength, `${at} must not be empty`);
    if (rule.pattern) assert.match(value, new RegExp(rule.pattern), `${at} must match ${rule.pattern}`);
  }
  if (Number.isInteger(value)) {
    if (rule.minimum !== undefined) assert.ok(value >= rule.minimum, `${at} must be >= ${rule.minimum}`);
    if (rule.maximum !== undefined) assert.ok(value <= rule.maximum, `${at} must be <= ${rule.maximum}`);
  }
  if (Array.isArray(value)) {
    if (rule.minItems !== undefined) assert.ok(value.length >= rule.minItems, `${at} must contain at least ${rule.minItems} item(s)`);
    if (rule.items) value.forEach((item, index) => assertSchemaValue(item, rule.items, `${at}[${index}]`));
  }
  if (schemaTypes(value) === "object") {
    for (const key of rule.required ?? []) assert.ok(Object.hasOwn(value, key), `${at}.${key} is required by schema`);
    if (rule.additionalProperties === false) for (const key of Object.keys(value)) assert.ok(Object.hasOwn(rule.properties ?? {}, key), `${at}.${key} is not declared by schema`);
    for (const [key, childRule] of Object.entries(rule.properties ?? {})) if (Object.hasOwn(value, key)) assertSchemaValue(value[key], childRule, `${at}.${key}`);
  }
}

function assertRange(range, name) {
  assert.equal(typeof range, "object", `${name} must be an object`);
  assert.ok("min" in range && "max" in range, `${name} must expose min and max`);
  for (const value of [range.min, range.max]) assert.ok(value === null || (Number.isInteger(value) && value >= 0), `${name} must contain non-negative integers or null`);
  if (range.min !== null && range.max !== null) assert.ok(range.min <= range.max, `${name} min must be <= max`);
}

function assertCard(card) {
  assertSchemaValue(card, schema, "exercise");
  const topBlocks = ["identity", "classification", "technique", "programming", "safety", "media", "review"];
  assert.deepEqual(Object.keys(card).sort(), [...topBlocks].sort(), "card must contain exactly the seven master-template blocks");
  const { identity, classification, technique, programming, safety, media, review } = card;
  assert.match(identity.exercise_id, /^exercise_\d{4}$/);
  assert.ok(Number.isInteger(identity.legacy_number));
  assert.match(identity.slug, /^[a-z0-9]+(?:-[a-z0-9]+)*$/);
  assert.equal(identity.schema_version, "1.1.0");
  assert.equal(identity.content_version, 1);
  assert.equal(identity.locale, "ru-RU");
  assert.equal(typeof identity.name.ru, "string");
  assert.ok(identity.name.ru.length > 0);
  assert.ok(identity.name.en === null || typeof identity.name.en === "string");
  assert.ok(["draft", "in_review", "ready", "published", "archived"].includes(identity.publication_status));
  assert.ok(dictionaries.sports.has(classification.sport));
  assert.ok(dictionaries.sections.has(classification.section));
  assert.ok(dictionaries.patterns.has(classification.movement_pattern));
  assert.ok(dictionaries.levels.has(classification.difficulty));
  assert.ok(dictionaries.profiles.has(classification.load_profile));
  for (const code of classification.equipment) assert.ok(dictionaries.equipment.has(code), `unknown equipment code ${code}`);
  for (const code of classification.anatomy.primary_muscles) assert.ok(dictionaries.muscles.has(code), `unknown muscle code ${code}`);
  for (const code of classification.anatomy.secondary_muscles) assert.ok(dictionaries.muscles.has(code), `unknown secondary muscle code ${code}`);
  for (const code of classification.anatomy.primary_targets) assert.ok(dictionaries.targets.has(code), `unknown target code ${code}`);
  assert.equal(typeof technique.source_key_cue, "string");
  assert.ok(Array.isArray(technique.steps));
  assert.ok(Array.isArray(technique.unfilled_fields));
  assert.ok(Array.isArray(technique.key_cues));
  assert.ok(["reps", "time", "distance", "cycles", "mixed"].includes(programming.type));
  for (const key of ["sets", "reps", "duration_seconds", "distance_meters", "cycles", "rest_seconds"]) assertRange(programming[key], `programming.${key}`);
  assert.ok(Array.isArray(safety.common_errors) && Array.isArray(safety.unfilled_fields));
  assert.equal(media.status, "missing");
  assert.ok(Array.isArray(media.phase_asset_ids) && Array.isArray(media.unfilled_fields));
  assert.equal(review.status, "draft");
  assert.ok(Array.isArray(review.sources) && Array.isArray(review.reviewers) && Array.isArray(review.unfilled_fields));
}

test("schema file declares the seven master-template blocks", () => {
  assert.equal(schema.$schema, "https://json-schema.org/draft/2020-12/schema");
  assert.deepEqual(schema.required.sort(), ["classification", "identity", "media", "programming", "review", "safety", "technique"]);
});

test("all 918 cards conform to the JSON contract", () => {
  for (const card of exercises) assertCard(card);
});

test("legacy ids are unique and retain the complete 1..918 range", () => {
  assert.equal(exercises.length, 918);
  const ids = exercises.map((card) => card.identity.exercise_id);
  const legacy = exercises.map((card) => card.identity.legacy_number).sort((a, b) => a - b);
  assert.equal(new Set(ids).size, 918);
  assert.deepEqual(legacy, Array.from({ length: 918 }, (_, index) => index + 1));
});

test("34 section exports contain all cards", () => {
  assert.equal(sections.length, 34);
  assert.equal(sections.reduce((sum, section) => sum + section.exercises.length, 0), 918);
  for (const section of sections) assert.ok(dictionaries.sections.has(section.section.code));
});

test("all referenced dictionary codes and 89 source equipment mappings exist", async () => {
  const sourceMappings = await readJson("dictionaries/equipment-source-mappings.json");
  assert.equal(sourceMappings.source_label_count, 89);
  assert.equal(sourceMappings.items.length, 89);
  for (const mapping of sourceMappings.items) for (const code of mapping.equipment_codes) assert.ok(dictionaries.equipment.has(code));
  const profiles = await readJson("dictionaries/load-profiles.json");
  assert.equal(profiles.source_label_count, 289);
  for (const profile of profiles.items) for (const code of profile.target_codes) assert.ok(dictionaries.targets.has(code));
});

test("all 28 duplicate names are retained as linked variants", () => {
  const variants = exercises.filter((card) => card.identity.variant_of !== null);
  assert.equal(variants.length, 28);
  const ids = new Set(exercises.map((card) => card.identity.exercise_id));
  for (const card of variants) {
    assert.ok(ids.has(card.identity.variant_of));
    assert.equal(card.identity.canonical_exercise_id, card.identity.variant_of);
  }
});
