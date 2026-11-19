#!/usr/bin/env python3
"""Gate: every person-name render outside the rota must be wrapped in shortName().

WHY THIS EXISTS AS A SCRIPT. Every site was found by hand and edited by line
number, and a MISS is invisible -- the page still renders, the name is just still
too long, so nothing fails and nothing looks wrong. There are no JS tests in this
repository and no linting step, so without this nothing would notice. It found
three real misses the first time it ran, in pages nobody thought to look at.

WHAT IT IS NOT. It is a source scan, not a browser check. It proves that every
person name that is PRINTED has been wrapped. It cannot prove that a newly added
page prints names through a helper it recognises, and it will not notice a new
person-name field at all -- `logged_by_name` is in the list because somebody added
it, and the next one will not be.

SCOPE IS BY COMPONENT, not by line number and not by a per-line marker. An earlier
version matched text on the line, which failed twice over: it could not tell
`shortName(alert.logged_by_name)` (wrapped) from a bare `logged_by_name`, and it
could not see that the rota's Avatar sits on a line carrying none of the rota
markers its neighbour does. Line numbers move whenever a comment is reworded;
component names do not.

IF YOU RENAME A COMPONENT, UPDATE EXEMPT_COMPONENTS. A renamed component stops
matching and the rota's own names get reported as misses -- which is a loud,
obvious failure rather than a silent one, which is the right way round for this
list to fail.

WHAT IT READS. Every module under cmd/api/ui/src/app, which the build concatenates
into one script; components never span two files, so each is scanned whole. The
scan resets at every file for that reason. Findings name the FILE as well as the
line, because a line number alone no longer identifies a place.
"""
import glob, os, re, sys

DIR = "cmd/api/ui/src/app"

# Read from the directory, not from a list kept here. build.mjs already fails if a
# module on disk is missing from its manifest, so a file this finds is a file that
# ships -- there is no second list to forget to update.
SOURCES = sorted(glob.glob(os.path.join(DIR, "*.jsx")))
if not SOURCES:
    sys.exit(f"  no source found in {DIR}")

EXEMPT_COMPONENTS = {
    # The rota grid and the page around it.
    "RotaTable", "SchedulePage",
    # The two dialogs the rota opens. Both print rota strings (rota-abs-who,
    # rotaAbsRemoveBody) and both are handed a member name straight from the rota,
    # so they belong to the rota's decision rather than to their own.
    "AbsenceDialog", "ConfirmRemoveAbsence",
    # Builds the issue view-model. loggedByName is read by exactly two render
    # sites, and both are wrapped, so shortening here would only add a second
    # place to forget rather than remove one.
    "mapIssue",
}
# Non-render uses of a name that must keep the whole value.
#
#   useState(currentUser.name)  the PROFILE EDIT FORM'S INITIAL VALUE, submitted
#     back to users.name. Wrapping it would quietly rewrite every full name in the
#     installation to its shortened form on the next profile save.
#
# The other two are local view objects whose single render site IS wrapped:
# onNextCell becomes onNext (printed by the issue form) and res.user.name becomes
# resetCode.name (printed by ResetCodeDialog). Wrapping at the source instead
# would be equally correct and would put the decision in a component that also
# does unrelated work.
TEXT_EXEMPT = re.compile(
    r"useState\(currentUser\.name\)"
    r"|onNextCell \? \{ name:"
    r"|setResetCode\(\{"
    # mapIssue's view-model build. Not detected as a component because the
    # component regex only matches capitalised names, and this one is lowercase --
    # so it is exempted here rather than by widening that regex, which would then
    # treat every local `const x = ...` as a new component and break the scoping
    # the whole file depends on.
    r"|loggedByName:\s"
)

COMPONENT = re.compile(r"^(?:function|const)\s+([A-Z]\w*)\s*[=(]")

# A person name, as the WHOLE dotted expression -- so that shortName(x.name) and
# shortName(res.user.name) both register as wrapped.
#
# The object is allowed to be DOTTED (`res.user.name`), and that is not tidiness:
# with a single-segment object the pattern matched only `user.name` inside
# `shortName(res.user.name)`, found ten characters of `res.` in front of it,
# concluded it was bare, and reported a site that was in fact wrapped. Two of the
# five "misses" on the first run were this and nothing else.
PERSON = re.compile(
    r"\b((?:[A-Za-z_$][\w$]*\.){1,3}"
    r"(?:name|logged_by_name|solved_by_name|added_by_name|done_by_name|assignee_name"
    r"|loggedByName))\b"
)
# `c.name` is consumables, `a`/`b` are sort comparators over catalog rows, and
# `users` is this script's own prose -- none of them a person on any page.
#
# `et` is an equipment TYPE on the Rooms page, and it is here under a new name:
# the rooms work renamed the old `ty` to `et`, and this list was not updated with
# it, so ten equipment names were reported as unwrapped people and the gate had
# been failing since. `device` is the handset identity (deviceIdentity), and `e`
# is the row of `c.equipment.map(e => ...)`; both single letters and catalog rows
# are the same exemption the existing entries already make.
NOT_PERSON_OBJ = {"c", "item", "draft", "shift", "s", "d", "a", "b", "e", "x", "ty",
                  "et", "device", "it", "ci", "value", "o", "t", "users", "department",
                  "connecta_agent", "consumable_item", "issue_type", "preset"}

WRAP = "shortName("

wrapped, missed, skipped = [], [], []
for path in SOURCES:
    # A component never spans two modules -- the build's cut points are all at
    # top-level statements -- so the current component is known again from the
    # first line of each file.
    current = None
    name = os.path.basename(path)
    lines = open(path, "rb").read().decode("utf-8").split("\n")
    for n, line in enumerate(lines, start=1):
        where = f"{name}:{n}"
        m = COMPONENT.match(line)
        if m:
            current = m.group(1)
        if line.lstrip().startswith("//") or "function shortName" in line:
            continue
        if current in EXEMPT_COMPONENTS or TEXT_EXEMPT.search(line):
            skipped.extend((where, hit.group(0)) for hit in PERSON.finditer(line))
            continue
        for hit in PERSON.finditer(line):
            # The segment IMMEDIATELY before the field, not the first one.
            # `res.department.name` and `m.shift.name` both start with an object that
            # looks like a person -- res, m -- and taking the first segment reported
            # both as unwrapped people when neither is. The question is always "what is
            # the thing being named", which is the last segment before the field.
            parts = hit.group(1).split(".")
            field, obj = parts[-1], parts[-2]
            if obj in NOT_PERSON_OBJ:
                continue
            start = hit.start()
            if line[max(0, start - len(WRAP)):start] == WRAP:
                wrapped.append((where, hit.group(1)))
            else:
                missed.append((where, hit.group(1), line.strip()[:100]))

print(f"  wrapped {len(wrapped)}, exempt {len(skipped)}, MISSED {len(missed)}")
for where, expr, text in missed:
    print(f"    {where}: {expr}")
    print(f"      {text}")
if missed:
    print("")
    print("  A person name is being printed outside the rota without shortName().")
    print("  If this is a NEW page, wrap it. If it is the rota, it has been renamed --")
    print("  add it to EXEMPT_COMPONENTS.")
sys.exit(1 if missed else 0)
