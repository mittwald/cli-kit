package notice

import (
	"fmt"

	"github.com/mittwald/cli-kit/theme"
)

// Role is what a value in a message is, such as a date or a path. Each role
// has a color of its own in every palette, for dark and light terminals and
// on the background of each level. The built-in roles are colored by the
// built-in palettes; [NewRole] makes a role a tool needs beyond them.
//
// Roles are compared by identity: two roles with the same name are still
// two roles.
type Role struct{ spec *roleSpec }

type roleSpec struct {
	name   string
	colors RoleColors
}

// RoleColors are the colors a role brings along for palettes that do not
// color it themselves.
type RoleColors struct {
	// Dark colors the role on a dark terminal.
	Dark theme.Style
	// Light colors the role on a light terminal.
	Light theme.Style
	// OnAccent colors the role on the background of a level, as
	// LayoutBlock shows it. A level it leaves out shows the role in the
	// level's own text color, bold, which reads on any background.
	OnAccent map[Level]theme.Style
}

// The built-in roles.
var (
	// RoleDate is a date, such as "19.11.2026".
	RoleDate = Role{&roleSpec{name: "date"}}
	// RoleTime is a time of day, such as "08:40".
	RoleTime = Role{&roleSpec{name: "time"}}
	// RoleDetail is any other value: an error's text, a count, a name.
	RoleDetail = Role{&roleSpec{name: "detail"}}
	// RoleRef names something: a merge request, a review, a session, a
	// branch.
	RoleRef = Role{&roleSpec{name: "ref"}}
	// RolePath is a file or a URL.
	RolePath = Role{&roleSpec{name: "path"}}
)

// Roles lists the built-in roles.
var Roles = []Role{RoleDate, RoleTime, RoleDetail, RoleRef, RolePath}

// NewRole makes a role of a tool's own, colored by colors wherever a palette
// does not color it. Dark and Light must both be set, so the role reads on
// either terminal; NewRole panics otherwise, at start-up rather than on the
// first light terminal.
func NewRole(name string, colors RoleColors) Role {
	if colors.Dark == "" || colors.Light == "" {
		panic(fmt.Sprintf("notice: role %q needs both a Dark and a Light color", name))
	}
	return Role{&roleSpec{name: name, colors: colors}}
}

// Name returns the name the role was made with.
func (r Role) Name() string {
	if r.spec == nil {
		return ""
	}
	return r.spec.name
}

// Value marks v as a value of role r in a message: it is shown in the
// role's color and is never searched by a [Pattern]. Like every other
// argument, its text is made plain (term.Plain) and has credentials taken
// out (redact.Patterns) unless the [Options] say otherwise.
func Value(r Role, v any) any { return value{role: r, v: v} }

// Date marks v as a date: [Value] with [RoleDate]. Time, Detail, Ref and
// Path do the same for the other built-in roles.
func Date(v any) any { return Value(RoleDate, v) }

// Time marks v as a time of day; see [Date].
func Time(v any) any { return Value(RoleTime, v) }

// Detail marks v as a detail, such as an error's text; see [Date].
func Detail(v any) any { return Value(RoleDetail, v) }

// Ref marks v as a reference, such as a merge request; see [Date].
func Ref(v any) any { return Value(RoleRef, v) }

// Path marks v as a file or a URL; see [Date].
func Path(v any) any { return Value(RolePath, v) }

type value struct {
	role Role
	v    any
}
