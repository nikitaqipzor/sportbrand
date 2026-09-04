#!/usr/bin/env python3
"""Build the versioned exercise catalog from the source Word workbook.

The output is intentionally data-only.  It preserves every source cell and never
invents medical, instructional, or media content that is absent from the workbook.
"""

from __future__ import annotations

import json
import re
import shutil
import unicodedata
from collections import Counter, defaultdict
from pathlib import Path

from docx import Document


ROOT = Path(__file__).resolve().parents[2]
SOURCE = ROOT / "product-materials" / "Единая_энциклопедия_упражнений_спорт_918(2).docx"
OUT = ROOT / "content"

SECTION_DEFINITIONS = [
    ("machines-and-cable-stations", "Тренажёры и блочные станции", "general_fitness"),
    ("bands-and-resistance", "Упражнения с резинками и эспандерами", "general_fitness"),
    ("pvc-and-stick", "Упражнения с гимнастической палкой / PVC", "general_fitness"),
    ("core", "Пресс и кор", "general_fitness"),
    ("warm-up-and-dynamic-mobility", "Разминка и динамическая мобильность", "general_fitness"),
    ("jump-rope", "Скакалка: техника и варианты прыжков", "general_fitness"),
    ("kettlebells", "Гири (kettlebell)", "general_fitness"),
    ("medicine-balls", "Медболы (medicine ball)", "general_fitness"),
    ("stability-ball", "Фитбол / stability ball", "general_fitness"),
    ("dumbbells", "Гантели", "general_fitness"),
    ("barbell", "Штанга", "general_fitness"),
    ("bodyweight-and-calisthenics", "Собственный вес / калистеника", "general_fitness"),
    ("trx-and-suspension", "TRX / подвесные петли", "general_fitness"),
    ("pull-up-bars-and-dips", "Турник и брусья", "general_fitness"),
    ("joint-mobility", "Мобильность суставов", "general_fitness"),
    ("cool-down", "Заминка", "general_fitness"),
    ("static-stretching", "Статическая растяжка", "general_fitness"),
    ("self-myofascial-release", "МФР: foam roller и массажный мяч", "general_fitness"),
    ("plyometrics-and-jumps", "Плиометрика и прыжки", "general_fitness"),
    ("balance-and-proprioception", "Баланс, BOSU и проприоцепция", "general_fitness"),
    ("battle-ropes", "Battle ropes / тяжёлые канаты", "general_fitness"),
    ("sled", "Сани / sled", "general_fitness"),
    ("sandbag", "Sandbag / тренировочный мешок", "general_fitness"),
    ("landmine", "Landmine", "general_fitness"),
    ("carries-grip-and-forearms", "Переноски, хват и предплечья", "general_fitness"),
    ("foot-and-ankle", "Стопа и голеностоп", "general_fitness"),
    ("agility-ladder-and-cones", "Лестница координации и конусы", "general_fitness"),
    ("breathing-and-recovery", "Дыхание и восстановление", "general_fitness"),
    ("swimming-technique-and-drills", "Плавание — техника и дриллы в воде", "swimming"),
    ("swimming-starts-turns-and-open-water", "Плавание — старты, повороты, подводная фаза и открытая вода", "swimming"),
    ("basketball-ball-handling-passing-and-shooting", "Баскетбол — владение мячом, передачи и бросок", "basketball"),
    ("basketball-footwork-defense-and-game-drills", "Баскетбол — футворк, защита, подбор и игровые дриллы", "basketball"),
    ("cycling-handling-and-group-technique", "Велоспорт — управление велосипедом и групповая техника", "cycling"),
    ("cycling-cadence-sprint-and-climbs", "Велоспорт — каденс, спринт, подъёмы и тренировочные дриллы", "cycling"),
]

SPORTS = [
    {"code": "general_fitness", "label_ru": "Общая физическая подготовка"},
    {"code": "swimming", "label_ru": "Плавание"},
    {"code": "basketball", "label_ru": "Баскетбол"},
    {"code": "cycling", "label_ru": "Велоспорт"},
]

LEVELS = [
    {"code": "beginner", "label_ru": "Начальный", "source_labels": ["Начальный"]},
    {"code": "intermediate", "label_ru": "Средний", "source_labels": ["Средний"]},
    {"code": "advanced", "label_ru": "Продвинутый", "source_labels": ["Продвинутый"]},
]
LEVEL_CODE = {label: item["code"] for item in LEVELS for label in item["source_labels"]}

MOVEMENT_PATTERNS = [
    ("squat", "Присед"), ("hinge", "Тазовый наклон / hinge"), ("push", "Жим"),
    ("pull", "Тяга"), ("carry", "Переноска"), ("rotate", "Ротация"),
    ("locomotion", "Локомоция"), ("jump", "Прыжок"), ("throw", "Бросок"),
    ("swim", "Плавательный дрилл"), ("pedal", "Педалирование"),
    ("mobility", "Мобильность"), ("stretch", "Растяжка"),
    ("balance", "Баланс"), ("breathing", "Дыхание"), ("other", "Другое"),
]

EQUIPMENT_KEYWORDS = [
    ("бассейн", "pool"), ("открытая вода", "open_water"), ("велосипед", "bicycle"),
    ("велотренажёр", "exercise_bike"), ("баскетбольный мяч", "basketball"),
    ("мяч", "ball"), ("кольцо", "basketball_hoop"), ("площадка", "court_or_training_area"),
    ("шлем", "helmet"), ("конусы", "cones"), ("agility ladder", "agility_ladder"),
    ("скакалка", "jump_rope"), ("battle ropes", "battle_ropes"), ("сани", "sled"),
    ("sandbag", "sandbag"), ("landmine", "landmine_attachment"), ("foam roller", "foam_roller"),
    ("массажный мяч", "massage_ball"), ("bosu", "bosu"), ("balance pad", "balance_pad"),
    ("fitball", "stability_ball"), ("фитбол", "stability_ball"), ("медбол", "medicine_ball"),
    ("trx", "suspension_trainer"), ("петли", "suspension_trainer"), ("турник", "pull_up_bar"),
    ("брусья", "dip_bars"), ("резин", "resistance_band"), ("бэнд", "resistance_band"),
    ("эспандер", "resistance_band"), ("стропа", "strap"), ("гир", "kettlebell"),
    ("гантел", "dumbbell"), ("штанг", "barbell"), ("блин", "weight_plate"),
    ("стойк", "rack"), ("скамь", "bench"), ("коврик", "exercise_mat"),
    ("стена", "wall"), ("опора", "support"), ("ступень", "step"),
    ("короб", "plyo_box"), ("палк", "training_stick"), ("pvc", "training_stick"),
    ("дорожка", "treadmill"), ("гребной", "rowing_machine"), ("кроссовер", "cable_crossover"),
    ("блок", "cable_machine"), ("смит", "smith_machine"), ("гравитрон", "assisted_pullup_dip_machine"),
    ("тренаж", "strength_machine"), ("машина", "strength_machine"), ("ролик для пресса", "ab_wheel"),
]

# These source labels are concrete apparatus names which cannot safely fall back
# to a broad keyword such as "machine".  The mapping is deliberately explicit.
EQUIPMENT_EXACT_CODES = {
    "Leg Press": ["leg_press_machine"], "Hack Squat": ["hack_squat_machine"],
    "Маятниковый присед": ["pendulum_squat_machine"], "Leg Extension": ["leg_extension_machine"],
    "Seated Leg Curl": ["seated_leg_curl_machine"], "Lying Leg Curl": ["lying_leg_curl_machine"],
    "Standing Leg Curl": ["standing_leg_curl_machine"], "Hip Abductor": ["hip_abductor_machine"],
    "Hip Adductor": ["hip_adductor_machine"], "Glute Machine": ["glute_machine"],
    "Hip Thrust Machine": ["hip_thrust_machine"], "Ягодичный тренажёр": ["glute_machine"],
    "Back Extension Machine": ["back_extension_machine"], "Гиперэкстензия": ["back_extension_bench"],
    "Standing Calf Raise": ["standing_calf_raise_machine"], "Seated Calf Raise": ["seated_calf_raise_machine"],
    "Calf Machine": ["calf_raise_machine"], "Tibialis Machine": ["tibialis_machine"],
    "Ab Crunch Machine": ["ab_crunch_machine"], "Rotary Torso": ["rotary_torso_machine"],
    "Собственный вес": ["bodyweight"], "Без оборудования": ["no_equipment"],
    "Планируемый снаряд": ["planned_equipment"],
}
EQUIPMENT_ALTERNATIVE_LABELS = {
    "Турник / брусья": [["pull_up_bar", "dip_bars"]],
    "Гантель / гиря": [["dumbbell", "kettlebell"]],
    "Гантели / гири": [["dumbbell", "kettlebell"]],
    "Гири / гантели": [["kettlebell", "dumbbell"]],
}


def write_json(path: Path, value: object) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(value, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")


def slugify(value: str) -> str:
    table = str.maketrans({
        "а":"a", "б":"b", "в":"v", "г":"g", "д":"d", "е":"e", "ё":"e", "ж":"zh", "з":"z", "и":"i", "й":"y", "к":"k", "л":"l", "м":"m", "н":"n", "о":"o", "п":"p", "р":"r", "с":"s", "т":"t", "у":"u", "ф":"f", "х":"h", "ц":"ts", "ч":"ch", "ш":"sh", "щ":"sch", "ъ":"", "ы":"y", "ь":"", "э":"e", "ю":"yu", "я":"ya",
        "А":"a", "Б":"b", "В":"v", "Г":"g", "Д":"d", "Е":"e", "Ё":"e", "Ж":"zh", "З":"z", "И":"i", "Й":"y", "К":"k", "Л":"l", "М":"m", "Н":"n", "О":"o", "П":"p", "Р":"r", "С":"s", "Т":"t", "У":"u", "Ф":"f", "Х":"h", "Ц":"ts", "Ч":"ch", "Ш":"sh", "Щ":"sch", "Ъ":"", "Ы":"y", "Ь":"", "Э":"e", "Ю":"yu", "Я":"ya",
    })
    value = unicodedata.normalize("NFKD", value.translate(table)).encode("ascii", "ignore").decode("ascii").lower()
    return re.sub(r"(^-|-$)", "", re.sub(r"[^a-z0-9]+", "-", value)) or "unnamed"


def unique_code(prefix: str, label: str, used: set[str]) -> str:
    base = f"{prefix}_{slugify(label).replace('-', '_')}"
    code, index = base, 2
    while code in used:
        code = f"{base}_{index}"
        index += 1
    used.add(code)
    return code


def normalise_name(value: str) -> str:
    return re.sub(r"\s+", " ", value.strip()).casefold()


def extract_rows() -> list[dict[str, str]]:
    document = Document(SOURCE)
    rows: list[dict[str, str]] = []
    for table_index, table in enumerate(document.tables[:34]):
        for row in table.rows[1:]:
            values = [cell.text.strip() for cell in row.cells]
            if len(values) != 7 or not values[0].isdigit():
                continue
            rows.append({
                "legacy_number": int(values[0]), "name": values[1], "load": values[2],
                "equipment": values[3], "level": values[4], "key_cue": values[5],
                "volume": values[6], "section_index": table_index,
            })
    if len(rows) != 918 or [item["legacy_number"] for item in rows] != list(range(1, 919)):
        raise ValueError("The source workbook must contain the sequential legacy numbers 1..918.")
    return rows


def classify_equipment(raw: str) -> dict[str, list]:
    if raw in EQUIPMENT_EXACT_CODES:
        return {"required": EQUIPMENT_EXACT_CODES[raw], "alternatives": []}
    if raw in EQUIPMENT_ALTERNATIVE_LABELS:
        choices = EQUIPMENT_ALTERNATIVE_LABELS[raw]
        return {"required": [], "alternatives": choices}
    lowered = raw.casefold()
    result: list[str] = []
    for needle, code in EQUIPMENT_KEYWORDS:
        if needle in lowered and code not in result:
            result.append(code)
    # A source label may be an intentionally broad contextual requirement.  It
    # still receives a deterministic code instead of a silent unspecified value.
    return {"required": result or [f"source_equipment_{slugify(raw).replace('-', '_')}"], "alternatives": []}


def movement_pattern(name: str, section_code: str) -> str:
    lower = name.casefold()
    rules = [
        (r"плав|кроль|брасс|баттерф|старт|поворот|streamline|sculling", "swim"),
        (r"cadence|велосипед|спринт|подъём|педал|тормож|chaingang", "pedal"),
        (r"прыж|jump|hop|boxer step|скакал", "jump"),
        (r"carry|переноск|ходьб|march", "carry"),
        (r"брос|pass|shot|dribble|lay-up", "throw"),
        (r"жим|push-up|отжим", "push"),
        (r"тяга|row|pull|подтяг", "pull"),
        (r"присед|squat|выпад|lunge", "squat"),
        (r"станов|deadlift|hinge|good morning", "hinge"),
        (r"ротац|twist|rotate", "rotate"),
        (r"растяж|stretch", "stretch"),
        (r"дых|breathing", "breathing"),
        (r"баланс|стойка на одной", "balance"),
        (r"мобил|mobility|cat-cow|pass-through", "mobility"),
    ]
    for pattern, code in rules:
        if re.search(pattern, lower):
            return code
    if section_code in {"warm-up-and-dynamic-mobility", "joint-mobility", "cool-down", "self-myofascial-release"}:
        return "mobility"
    return "other"


def parse_target_labels(raw: str) -> list[str]:
    normalized = raw.replace("/", ",").replace(";", ",")
    parts = [re.sub(r"\s+", " ", part).strip(" ,") for part in normalized.split(",")]
    return [part for part in parts if part]


def target_kind(label: str) -> str:
    """Classify only the source label's role; do not infer new anatomy."""
    anatomy_words = (
        "груд", "трицеп", "дельт", "трапец", "ромбовид", "широчайш", "бицепс",
        "предплеч", "кор", "пресс", "спин", "ягод", "квадрицепс", "бедр", "икр",
        "стоп", "голен", "мышц", "сгибател", "аддукт", "плеч", "ротатор", "зубчат",
        "шея", "ног", "рук", "таз", "тулов", "грудной отдел", "всё тело",
    )
    return "muscle_or_body_region" if any(word in label.casefold() for word in anatomy_words) else "training_target"


def parse_volume(raw: str) -> dict:
    text = re.sub(r"\s+", " ", raw.replace("–", "-").replace("×", "x")).strip()
    result = {
        "raw": raw, "type": "mixed", "sets": {"min": None, "max": None},
        "reps": {"min": None, "max": None}, "duration_seconds": {"min": None, "max": None},
        "distance_meters": {"min": None, "max": None}, "cycles": {"min": None, "max": None},
        "rounds": {"min": None, "max": None}, "passes": {"min": None, "max": None},
        "attempts": {"min": None, "max": None}, "rest_seconds": {"min": None, "max": None},
        "work_rest_intervals": [], "intensity": {"type": None, "min": None, "max": None},
        "completion_conditions": None, "unfilled_fields": [],
    }
    set_match = re.search(r"(\d+)\s*-\s*(\d+)\s*x", text, re.I)
    if set_match:
        result["sets"] = {"min": int(set_match.group(1)), "max": int(set_match.group(2))}
    unit_pattern = r"(?P<unit>мин(?:ут(?:ы|а)?)?\.?|с(?:ек(?:унд(?:ы|а)?)?)?\.?|м\b|повтор(?:ов|а)?|цик(?:л(?:ов|а)?)?|раунд(?:ов|а)?|проход(?:ов|а)?|попыт(?:ок|ки)?)"
    range_pattern = re.compile(rf"(?P<min>\d+)\s*-\s*(?P<max>\d+)(?!\d)\s*{unit_pattern}", re.I)
    measurements = []
    for match in range_pattern.finditer(text):
        start, end = int(match.group("min")), int(match.group("max"))
        unit = match.group("unit").casefold().rstrip(".")
        before, after = text[max(0, match.start() - 16):match.start()].casefold(), text[match.end():match.end() + 16].casefold()
        if unit.startswith("мин"):
            key, values = "duration_seconds", {"min": start * 60, "max": end * 60}
        elif unit.startswith("с"):
            key, values = "duration_seconds", {"min": start, "max": end}
        elif unit == "м":
            key, values = "distance_meters", {"min": start, "max": end}
        elif unit.startswith("повтор"):
            key, values = "reps", {"min": start, "max": end}
        elif unit.startswith("цик"):
            key, values = "cycles", {"min": start, "max": end}
        elif unit.startswith("раунд"):
            key, values = "rounds", {"min": start, "max": end}
        elif unit.startswith("проход"):
            key, values = "passes", {"min": start, "max": end}
        else:
            key, values = "attempts", {"min": start, "max": end}
        result[key] = values
        measurements.append((key, values, before, after))
    # An x-range without an explicit physical unit is the only case mapped to reps.
    rep_match = re.search(r"x\s*(\d+)\s*-\s*(\d+)(?!\d)", text, re.I)
    if rep_match:
        following = text[rep_match.end():].lstrip().casefold()
        explicit_unit = re.match(r"(?:мин|с(?:ек)?|м\b|повтор|цик|раунд|проход|попыт)", following)
        if not explicit_unit:
            result["reps"] = {"min": int(rep_match.group(1)), "max": int(rep_match.group(2))}
    # Work/rest forms retain both values rather than turning rest into repetitions.
    time_measurements = [(values, before, after) for key, values, before, after in measurements if key == "duration_seconds"]
    work = next((values for values, before, after in time_measurements if "работ" in before + after), None)
    rest = next((values for values, before, after in time_measurements if "отдых" in before + after), None)
    if work and rest:
        result["work_rest_intervals"] = [{"work_seconds": work, "rest_seconds": rest}]
        result["rest_seconds"] = rest
    # Mixed-unit range, e.g. "10 с - 30 мин", is common in cycling intervals.
    mixed_time = re.search(r"(\d+)\s*с\s*-\s*(\d+)\s*мин", text, re.I)
    if mixed_time:
        result["duration_seconds"] = {"min": int(mixed_time.group(1)), "max": int(mixed_time.group(2)) * 60}
    populated = [key for key in ["reps", "duration_seconds", "distance_meters", "cycles", "rounds", "passes", "attempts"] if result[key]["min"] is not None]
    if len(populated) == 1:
        result["type"] = {"reps": "reps", "duration_seconds": "time", "distance_meters": "distance", "cycles": "cycles"}.get(populated[0], "mixed")
    elif not populated:
        result["unfilled_fields"].append("structured_volume")
    return result


def schema() -> dict:
    nullable_string = {"type": ["string", "null"]}
    nullable_integer = {"type": ["integer", "null"], "minimum": 0}
    range_schema = {"type": "object", "required": ["min", "max"], "additionalProperties": False, "properties": {"min": nullable_integer, "max": nullable_integer}}
    return {
        "$schema": "https://json-schema.org/draft/2020-12/schema",
        "$id": "https://athletica.ai/content/schema/exercise.schema.json",
        "title": "Athletica AI exercise card",
        "type": "object",
        "additionalProperties": False,
        "required": ["identity", "classification", "technique", "programming", "safety", "media", "review"],
        "properties": {
            "identity": {"type": "object", "additionalProperties": False, "required": ["exercise_id", "legacy_number", "slug", "schema_version", "content_version", "locale", "name", "aliases", "canonical_exercise_id", "variant_of", "publication_status"], "properties": {
                "exercise_id": {"type": "string", "pattern": "^exercise_[0-9]{4}$"}, "legacy_number": {"type": "integer", "minimum": 1, "maximum": 918}, "slug": {"type": "string", "pattern": "^[a-z0-9]+(?:-[a-z0-9]+)*$"}, "schema_version": {"const": "1.1.0"}, "content_version": {"const": 1}, "locale": {"const": "ru-RU"}, "name": {"type": "object", "required": ["ru", "en"], "additionalProperties": False, "properties": {"ru": {"type": "string", "minLength": 1}, "en": nullable_string}}, "aliases": {"type": "array", "items": {"type": "string"}}, "canonical_exercise_id": nullable_string, "variant_of": nullable_string, "publication_status": {"enum": ["draft", "in_review", "ready", "published", "archived"]}
            }},
            "classification": {"type": "object", "additionalProperties": False, "required": ["sport", "section", "movement_pattern", "difficulty", "equipment", "equipment_alternatives", "load_profile", "anatomy"], "properties": {"sport": {"type": "string"}, "section": {"type": "string"}, "movement_pattern": {"type": "string"}, "difficulty": {"enum": ["beginner", "intermediate", "advanced"]}, "equipment": {"type": "array", "items": {"type": "string"}}, "equipment_alternatives": {"type": "array", "items": {"type": "array", "minItems": 1, "items": {"type": "string"}}}, "load_profile": {"type": "string"}, "anatomy": {"type": "object", "additionalProperties": False, "required": ["primary_muscles", "secondary_muscles", "primary_targets", "joints", "laterality", "unfilled_fields"], "properties": {"primary_muscles": {"type": "array", "items": {"type": "string"}}, "secondary_muscles": {"type": "array", "items": {"type": "string"}}, "primary_targets": {"type": "array", "items": {"type": "string"}}, "joints": {"type": "array", "items": {"type": "string"}}, "laterality": {"enum": ["bilateral", "left", "right", "alternating", "not_applicable", None]}, "unfilled_fields": {"type": "array", "items": {"type": "string"}}}}}},
            "technique": {"type": "object", "additionalProperties": False, "required": ["source_key_cue", "setup", "start_position", "steps", "key_cues", "breathing", "tempo", "range_of_motion", "finish", "unfilled_fields"], "properties": {"source_key_cue": {"type": "string"}, "setup": nullable_string, "start_position": nullable_string, "steps": {"type": "array", "items": {"type": "string"}}, "key_cues": {"type": "array", "items": {"type": "string"}}, "breathing": nullable_string, "tempo": nullable_string, "range_of_motion": nullable_string, "finish": nullable_string, "unfilled_fields": {"type": "array", "items": {"type": "string"}}}},
            "programming": {"type": "object", "additionalProperties": False, "required": ["raw", "type", "sets", "reps", "duration_seconds", "distance_meters", "cycles", "rounds", "passes", "attempts", "rest_seconds", "work_rest_intervals", "intensity", "completion_conditions", "unfilled_fields"], "properties": {"raw": {"type": "string"}, "type": {"enum": ["reps", "time", "distance", "cycles", "mixed"]}, "sets": range_schema, "reps": range_schema, "duration_seconds": range_schema, "distance_meters": range_schema, "cycles": range_schema, "rounds": range_schema, "passes": range_schema, "attempts": range_schema, "rest_seconds": range_schema, "work_rest_intervals": {"type": "array", "items": {"type": "object", "additionalProperties": False, "required": ["work_seconds", "rest_seconds"], "properties": {"work_seconds": range_schema, "rest_seconds": range_schema}}}, "intensity": {"type": "object", "additionalProperties": False, "required": ["type", "min", "max"], "properties": {"type": nullable_string, "min": nullable_string, "max": nullable_string}}, "completion_conditions": nullable_string, "unfilled_fields": {"type": "array", "items": {"type": "string"}}}},
            "safety": {"type": "object", "additionalProperties": False, "required": ["common_errors", "stop_signals", "limitations", "regressions", "progressions", "injury_notes", "unfilled_fields"], "properties": {"common_errors": {"type": "array", "items": {"type": "string"}}, "stop_signals": {"type": "array", "items": {"type": "string"}}, "limitations": {"type": "array", "items": {"type": "string"}}, "regressions": {"type": "array", "items": {"type": "string"}}, "progressions": {"type": "array", "items": {"type": "string"}}, "injury_notes": nullable_string, "unfilled_fields": {"type": "array", "items": {"type": "string"}}}},
            "media": {"type": "object", "additionalProperties": False, "required": ["main_asset_id", "phase_asset_ids", "muscle_layer_asset_id", "animation_asset_id", "video_url", "view", "crop", "alt_text", "license", "technique_version", "status", "unfilled_fields"], "properties": {"main_asset_id": nullable_string, "phase_asset_ids": {"type": "array", "items": {"type": "string"}}, "muscle_layer_asset_id": nullable_string, "animation_asset_id": nullable_string, "video_url": nullable_string, "view": nullable_string, "crop": {"type": ["object", "null"]}, "alt_text": nullable_string, "license": nullable_string, "technique_version": nullable_string, "status": {"enum": ["missing", "draft", "method_checked", "approved"]}, "unfilled_fields": {"type": "array", "items": {"type": "string"}}}},
            "review": {"type": "object", "additionalProperties": False, "required": ["sources", "reviewers", "author_id", "editor_id", "status", "reviewed_at", "comment", "rejection_reason", "unfilled_fields"], "properties": {"sources": {"type": "array", "items": {"type": "object"}}, "reviewers": {"type": "array", "items": {"type": "object"}}, "author_id": nullable_string, "editor_id": nullable_string, "status": {"enum": ["draft", "method_review", "medical_review", "approved", "rejected"]}, "reviewed_at": nullable_string, "comment": nullable_string, "rejection_reason": nullable_string, "unfilled_fields": {"type": "array", "items": {"type": "string"}}}}
        }
    }


def main() -> None:
    rows = extract_rows()
    if OUT.exists():
        for path in [OUT / "schema", OUT / "dictionaries", OUT / "exercises"]:
            if path.exists():
                shutil.rmtree(path)
    write_json(OUT / "schema" / "exercise.schema.json", schema())

    # Equipment: each of the 89 original labels is preserved in a mapping, while
    # cards refer only to canonical equipment codes.
    raw_equipment = list(dict.fromkeys(row["equipment"] for row in rows))
    equipment_codes = {"unspecified_equipment"}
    source_mappings = []
    for raw in raw_equipment:
        normalized = classify_equipment(raw)
        codes = normalized["required"] + [code for group in normalized["alternatives"] for code in group]
        equipment_codes.update(codes)
        source_mappings.append({"source_label": raw, "equipment_codes": codes, "required_equipment_codes": normalized["required"], "equipment_alternatives": normalized["alternatives"]})
    labels_by_code = defaultdict(list)
    for mapping in source_mappings:
        for code in mapping["equipment_codes"]:
            labels_by_code[code].append(mapping["source_label"])
    write_json(OUT / "dictionaries" / "equipment.json", {"version": "1.1.0", "items": [{"code": code, "label_ru": code.replace("_", " "), "source_labels": sorted(labels_by_code[code])} for code in sorted(equipment_codes)]})
    write_json(OUT / "dictionaries" / "equipment-source-mappings.json", {"version": "1.1.0", "source_label_count": len(source_mappings), "items": source_mappings})

    # "Основная нагрузка" is not always anatomy (for example, basketball skills).
    # The source phrase therefore becomes a load profile and its exact target labels
    # become neutral target-group codes rather than fabricated muscle anatomy.
    target_code_by_label: dict[str, str] = {}
    used_target_codes: set[str] = set()
    load_profile_by_label: dict[str, str] = {}
    used_profile_codes: set[str] = set()
    profiles = []
    for raw_load in dict.fromkeys(row["load"] for row in rows):
        targets = []
        for label in parse_target_labels(raw_load):
            key = normalise_name(label)
            if key not in target_code_by_label:
                target_code_by_label[key] = unique_code("target", label, used_target_codes)
            targets.append(target_code_by_label[key])
        code = unique_code("load", raw_load, used_profile_codes)
        load_profile_by_label[raw_load] = code
        profiles.append({"code": code, "label_ru": raw_load, "target_codes": targets})
    target_labels = {normalise_name(label): label for row in rows for label in parse_target_labels(row["load"])}
    target_items = [{"code": code, "label_ru": target_labels[key], "kind": target_kind(target_labels[key])} for key, code in sorted(target_code_by_label.items(), key=lambda item: item[1])]
    target_kind_by_code = {item["code"]: item["kind"] for item in target_items}
    write_json(OUT / "dictionaries" / "load-targets.json", {"version": "1.1.0", "items": target_items})
    write_json(OUT / "dictionaries" / "muscle-groups.json", {"version": "1.1.0", "items": [item for item in target_items if item["kind"] == "muscle_or_body_region"]})
    write_json(OUT / "dictionaries" / "load-profiles.json", {"version": "1.1.0", "source_label_count": len(profiles), "items": profiles})
    write_json(OUT / "dictionaries" / "levels.json", {"version": "1.1.0", "items": LEVELS})
    write_json(OUT / "dictionaries" / "movement-patterns.json", {"version": "1.1.0", "items": [{"code": code, "label_ru": label} for code, label in MOVEMENT_PATTERNS]})
    write_json(OUT / "dictionaries" / "sports.json", {"version": "1.1.0", "items": SPORTS})
    write_json(OUT / "dictionaries" / "sections.json", {"version": "1.1.0", "items": [{"code": code, "legacy_section_number": index + 1, "label_ru": label, "sport": sport} for index, (code, label, sport) in enumerate(SECTION_DEFINITIONS)]})

    name_groups = defaultdict(list)
    for row in rows:
        name_groups[normalise_name(row["name"])].append(row)
    canonical_by_number: dict[int, str] = {}
    for group in name_groups.values():
        canonical_id = f"exercise_{min(row['legacy_number'] for row in group):04d}"
        if len(group) > 1:
            for row in group:
                canonical_by_number[row["legacy_number"]] = canonical_id
    variants = {number for number, canonical in canonical_by_number.items() if f"exercise_{number:04d}" != canonical}
    if len(variants) != 28:
        raise ValueError(f"Expected 28 duplicate-name variants, got {len(variants)}")

    sections = defaultdict(list)
    for row in rows:
        section_code, _, sport = SECTION_DEFINITIONS[row["section_index"]]
        exercise_id = f"exercise_{row['legacy_number']:04d}"
        canonical_id = canonical_by_number.get(row["legacy_number"])
        targets = [target_code_by_label[normalise_name(label)] for label in parse_target_labels(row["load"])]
        primary_muscles = [code for code in targets if target_kind_by_code[code] == "muscle_or_body_region"]
        equipment = classify_equipment(row["equipment"])
        record = {
            "identity": {
                "exercise_id": exercise_id, "legacy_number": row["legacy_number"],
                "slug": f"exercise-{row['legacy_number']:04d}-{slugify(row['name'])}", "schema_version": "1.1.0",
                "content_version": 1, "locale": "ru-RU", "name": {"ru": row["name"], "en": None},
                "aliases": [], "canonical_exercise_id": canonical_id,
                "variant_of": canonical_id if canonical_id and canonical_id != exercise_id else None,
                "publication_status": "draft",
            },
            "classification": {
                "sport": sport, "section": section_code, "movement_pattern": movement_pattern(row["name"], section_code),
                "difficulty": LEVEL_CODE[row["level"]], "equipment": equipment["required"], "equipment_alternatives": equipment["alternatives"],
                "load_profile": load_profile_by_label[row["load"]],
                "anatomy": {"primary_muscles": primary_muscles, "secondary_muscles": [], "primary_targets": targets, "joints": [], "laterality": None, "unfilled_fields": ["secondary_muscles", "joints", "laterality"]},
            },
            "technique": {
                "source_key_cue": row["key_cue"], "setup": None, "start_position": None, "steps": [],
                "key_cues": [cue.strip() for cue in row["key_cue"].split(";") if cue.strip()], "breathing": None,
                "tempo": None, "range_of_motion": None, "finish": None,
                "unfilled_fields": ["setup", "start_position", "steps", "breathing", "tempo", "range_of_motion", "finish"],
            },
            "programming": parse_volume(row["volume"]),
            "safety": {
                "common_errors": [], "stop_signals": [], "limitations": [], "regressions": [], "progressions": [],
                "injury_notes": None, "unfilled_fields": ["common_errors", "stop_signals", "limitations", "regressions", "progressions", "injury_notes"],
            },
            "media": {
                "main_asset_id": None, "phase_asset_ids": [], "muscle_layer_asset_id": None, "animation_asset_id": None,
                "video_url": None, "view": None, "crop": None, "alt_text": None, "license": None,
                "technique_version": None, "status": "missing",
                "unfilled_fields": ["main_asset_id", "phase_asset_ids", "muscle_layer_asset_id", "animation_asset_id", "video_url", "view", "crop", "alt_text", "license", "technique_version"],
            },
            "review": {
                "sources": [], "reviewers": [], "author_id": None, "editor_id": None, "status": "draft",
                "reviewed_at": None, "comment": None, "rejection_reason": None,
                "unfilled_fields": ["sources", "reviewers", "author_id", "editor_id", "reviewed_at", "comment", "rejection_reason"],
            },
        }
        sections[section_code].append(record)

    catalog_index = {"by_exercise_id": {}, "by_legacy_number": {}, "by_slug": {}}
    for index, (section_code, label, sport) in enumerate(SECTION_DEFINITIONS, start=1):
        exercises = sections[section_code]
        filename = f"{index:02d}-{section_code}.json"
        write_json(OUT / "exercises" / filename, {
            "schema_version": "1.1.0", "section": {"code": section_code, "legacy_section_number": index, "label_ru": label, "sport": sport}, "exercises": exercises,
        })
        for offset, exercise in enumerate(exercises):
            identity = exercise["identity"]
            lookup = {"file": f"exercises/{filename}", "offset": offset, "legacy_number": identity["legacy_number"], "slug": identity["slug"], "section": section_code}
            catalog_index["by_exercise_id"][identity["exercise_id"]] = lookup
            catalog_index["by_legacy_number"][str(identity["legacy_number"])] = identity["exercise_id"]
            catalog_index["by_slug"][identity["slug"]] = identity["exercise_id"]

    write_json(OUT / "catalog.json", {
        "schema_version": "1.1.0", "content_locale": "ru-RU", "exercise_count": len(rows), "section_count": len(SECTION_DEFINITIONS),
        "lookup_index": "index.json", "source": {"document": SOURCE.name, "legacy_number_range": [1, 918]},
    })
    write_json(OUT / "index.json", {"schema_version": "1.1.0", "exercise_count": len(rows), **catalog_index})


if __name__ == "__main__":
    main()
