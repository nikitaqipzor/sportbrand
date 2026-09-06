// Package seed embeds the starter exercise catalogue.
//
// The real reference book — 918 exercises in 34 sections — is produced by a
// separate team into `content/` and is loaded with `api seed-exercises --file`.
// Until it lands, the client still has to have something to choose from, and
// the twenty records here are that something.
//
// They carry **the identifiers the app already uses**, taken verbatim from
// apps/mobile/src/features/workout/exercise-catalog.ts. That is not a
// convenience: those strings have already left the phone inside
// `clientMutationId` (`workoutId:exerciseId:setNumber`) and are stored in
// recorded sets, so the catalogue that replaces the hard-coded list must keep
// them or detach history from the exercises it was performed with.
//
// They carry no methodology. Technique, common errors, contraindications and
// media are empty, because the source they will come from has not been
// converted yet and a plausible invention is worse than a blank: people train
// on what this says.
package seed

import _ "embed"

// StarterExercises is the JSON import file for the twenty exercises the app
// shipped with. It is a normal import file — the same contract `content/`
// produces — so seeding it and seeding the real book go through one code path.
//
//go:embed exercises.starter.json
var StarterExercises []byte

// StarterSource names the embedded file in import records and log lines.
const StarterSource = "embedded:seed/exercises.starter.json"
