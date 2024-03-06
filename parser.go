package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

type Switch struct {
	Name            string
	Names           []string
	Value           []string
	Nargs           string
	N               int
	Enum            []string
	Map             func(s string) string
	Assert          func(s string) (bool, string)
	Requires        []string
	Excludes        []string
	AllowDuplicates bool
	Optional        bool
	Help            string
	Required        bool
	Found           bool
	Metavar         string
}

type Positional struct {
	Name    string
	Metavar string
	Assert  func(s string) (bool, string)
	Map     func(s string) string
	Help    string
	Value   string
	Enum    []string
}

type Names []string

type Parser struct {
	Argv        []string
	Desc        string
	Header      string
	Footer      string
	ExitOnHelp  bool
	Switches    map[string]*Switch
	Positionals map[string]*Positional
}

type switch_ref struct {
	ref  *Switch
	pos  int
	args []string
}

//////////////////////////////////////////////////
var ErrMissingName = errors.New("expected short and/or long name")
var ErrNoArgs = errors.New("no arguments passed")
var ErrExcessArgs = errors.New("excess arguments passed")
var ErrLessArgs = errors.New("not enough arguments passed")
var ErrLessPositionalArgs = errors.New("not enough positional arguments passed")
var ErrDuplicate = errors.New("cannot pass this switch more than once")
var ErrInvalidNargs = errors.New("need any of +, ?, *, <int>")
var ErrAssertionFailure = errors.New("assertion failed")
var ErrInvalidChoice = errors.New("invalid choice")
var ErrMissingDeps = errors.New("missing dependencies")
var ErrUnallowedDeps = errors.New("unallowed dependencies passed")
var ErrNameConflict = errors.New("cannot use the same name for positional args and switches")

//////////////////////////////////////////////////
var num_re = regexp.MustCompile("^[0-9]+$")
var nargs_re = regexp.MustCompile("^[+*?]+$")
var end_of_args_re = regexp.MustCompile("^--$")
var switches = map[string]*Switch{}
var positionals = map[string]*Positional{}
var positionals_slices = []*Positional{}
var parsed = map[string]*switch_ref{}
var parsed_slices = []*switch_ref{}
var printf = fmt.Printf
var head_argv = []string{}
var tail_argv = []string{}

func errf(err error, obj *Switch) {
	panic(fmt.Sprintf("%v\nSwitch: %#v\n", err, obj))
}

func perrf(err error, obj *Positional) {
	panic(fmt.Sprintf("%v\nPositional: %#v\n", err, obj))
}

//////////////////////////////////////////////////
func NewParser(argv []string, exit_on_help bool) *Parser {
	if argv == nil {
		argv = flag.Args()
	}

	double_dash := slices.Index(argv, "--")
	if double_dash != -1 {
		tail_argv = argv[double_dash+1:]
		argv = argv[:double_dash]
	}

	parser := &Parser{
		Argv:       argv,
		Switches:   map[string]*Switch{},
		ExitOnHelp: exit_on_help,
	}

	parser.Switch(
		Names{"h", "help"},
		&Switch{Help: "show this help"},
	)

	return parser
}

func (parser *Parser) Switch(names []string, opts *Switch) *Parser {
	names_l := len(names)
	if names_l == 0 {
		errf(ErrMissingName, opts)
	} else if names_l == 1 {
		names = append(names, "")
	}

	if (len(names) == 0) || ((names[0] == "") && (names[1] == "")) {
		errf(ErrMissingName, opts)
	} else if names[1] != "" {
		opts.Name = names[1]
	} else if names[0] != "" {
		opts.Name = names[0]
	}

	for _, n := range names {
		if _, ok := positionals[n]; ok {
			errf(ErrNameConflict, opts)
		}
	}

	opts.Names = names
	nargs := &opts.Nargs

	if *nargs != "" {
		if nargs_re.FindStringIndex(*nargs) == nil {
			errf(ErrInvalidNargs, opts)
		}
		opts.N = -1
	}

	switches[opts.Name] = opts
	opts.find(parser)

	return parser
}

func (parser *Parser) Required(names []string, opts *Switch) *Parser {
	opts.Required = true
	return parser.Switch(names, opts)
}

func (parser *Parser) Positional(name string, opts *Positional) *Parser {
	if _, ok := switches[name]; ok {
		perrf(ErrNameConflict, opts)
	}

	opts.Name = name
	positionals_slices = append(positionals_slices, opts)
	positionals[name] = opts

	return parser
}

func (S *Switch) find(parser *Parser) []int {
	exit_on_help := parser.ExitOnHelp
	argv := parser.Argv
	pos := []int{}
	dup := S.AllowDuplicates
	req := S.Required
	names := S.Names
	matches := func(prefix string, a string, b string) bool {
		a = strings.Join([]string{prefix, a}, "")
		return a == b
	}

	for i, v := range argv {
		short_matched := false

		if names[0] != "" && matches("-", names[0], v) {
			if v == "-h" && exit_on_help {
				fmt.Println(parser.String(true))
				os.Exit(0)
			}
			pos = append(pos, i)
			short_matched = true
		}

		if !short_matched && matches("--", names[1], v) {
			if v == "--help" && exit_on_help {
				fmt.Println(parser.String(false))
				os.Exit(0)
			}
			pos = append(pos, i)
		}

		if len(pos) == 0 && req {
			errf(ErrNoArgs, S)
		}
	}

	if len(pos) > 1 && !dup {
		errf(ErrDuplicate, S)
	}

	if len(pos) > 0 {
		for _, v := range pos {
			parsed_slices = append(parsed_slices, &switch_ref{
				ref:  S,
				pos:  v,
				args: []string{},
			})
		}
	}

	S.Found = true
	return pos
}

func (parser *Parser) extract() {
	slices.SortFunc(
		parsed_slices,
		func(a *switch_ref, b *switch_ref) int {
			if a.pos < b.pos {
				return -1
			}
			return 1
		},
	)

	parsed_slices_l := len(parsed_slices)
	first := parsed_slices[0]
	last := parsed_slices[parsed_slices_l-1]
	argv := parser.Argv
	l := len(argv)
	validate_n := func(S *Switch) {
		n := S.N
		nargs := S.Nargs
		args := S.Value
		gotten := len(args)

		if (n == 0 || nargs == "?" || nargs == "*") && gotten == 0 {
			return
		} else if n != -1 {
			if n > gotten {
				errf(ErrLessArgs, S)
			} else if n < gotten {
				errf(ErrExcessArgs, S)
			}
			return
		}

		switch nargs {
		case "+":
			if gotten == 0 {
				errf(ErrLessArgs, S)
			}
		case "?":
			if gotten > 1 {
				errf(ErrExcessArgs, S)
			}
		}
	}

	if first.pos != 0 {
		head_argv = argv[:first.pos]
	}

	if last.pos != l-1 {
		last.args = argv[last.pos+1:]
	}

	for i := 0; i < parsed_slices_l-1; i++ {
		current := parsed_slices[i]
		next := parsed_slices[i+1]
		current.args = argv[current.pos+1 : next.pos]
	}

	for i := 0; i < parsed_slices_l-1; i++ {
		v := parsed_slices[i]
		ref := v.ref
		ref.Value = append(ref.Value, v.args...)
	}

	for _, v := range switches {
		if !v.Found || v == last.ref {
			continue
		}
		validate_n(v)
	}

	last_args := last.args
	last_gotten := len(last_args)
	last_nargs := last.ref.Nargs
	last_n := last.ref.N
	last.ref.Value = last.args

	if last_n != -1 {
		if last_gotten == 0 && last_n != 0 {
			errf(ErrLessArgs, last.ref)
		} else if last_n > last_gotten {
			errf(ErrLessArgs, last.ref)
		} else {
			last_args = argv[last.pos+1 : last.pos+last_gotten]
			tail_argv = append(argv[last.pos+last_gotten:], tail_argv...)
			last.ref.Value = last_args
		}
	}

	switch last_nargs {
	case "+":
		if last_gotten == 0 {
			errf(ErrLessArgs, last.ref)
		}
	case "*":
		if last_gotten > 1 {
			errf(ErrExcessArgs, last.ref)
		}
	}

	head_argv = append(head_argv, tail_argv...)
	argv_l := len(head_argv)
	pos := positionals_slices
	pos_l := len(pos)

	if pos_l < argv_l {
		for i := pos_l; i < argv_l; i++ {
			parser.Positional(strconv.Itoa(i), &Positional{})
		}
	} else {
		panic(
			fmt.Errorf(
				"%w\nexpected %d arguments, got %d\n",
				ErrLessPositionalArgs,
				pos_l,
				argv_l,
			),
		)
	}

	for i, v := range head_argv {
		positionals_slices[i].Value = v
	}
}

func (parser *Parser) process_positional() {
	for _, v := range positionals_slices {
		a := v.Value

		if v.Enum != nil {
			if slices.Index(v.Enum, a) == -1 {
				panic(fmt.Sprintf(
					"%v\nChoices: %s\n%#v\n",
					ErrInvalidChoice,
					strings.Join(v.Enum, ","),
					v,
				))
			} else {
				continue
			}
		}

		if v.Assert != nil {
			if ok, msg := v.Assert(a); !ok {
				panic(fmt.Sprintf(
					"%v\nMessage: %s\n%#v\n",
					ErrAssertionFailure,
					msg,
					v,
				))
			}
		}

		if v.Map != nil {
			v.Value = v.Map(a)
		}
	}
}

func (parser *Parser) process_switches() {
	for _, v := range switches {
		deps := v.Requires
		excludes := v.Excludes

		if excludes != nil {
			for _, d := range excludes {
				_, ok := switches[d]
				if ok {
					panic(fmt.Sprintf(
						"%v\nUnallowed switch: %s\n%#v\n",
						ErrUnallowedDeps,
						d,
						v,
					))
				}
			}
		}

		if deps != nil {
			for _, d := range deps {
				_, ok := switches[d]
				if !ok {
					panic(fmt.Sprintf(
						"%v\nMissing switch: %s\n%#v\n",
						ErrMissingDeps,
						d,
						v,
					))
				}
			}
		}

		for i, a := range v.Value {
			if v.Enum != nil {
				if slices.Index(v.Enum, a) == -1 {
					panic(fmt.Sprintf(
						"%v\nChoices: %s\n%#v\n",
						ErrInvalidChoice,
						strings.Join(v.Enum, ","),
						v,
					))
				} else {
					continue
				}
			}

			if v.Assert != nil {
				if ok, msg := v.Assert(a); !ok {
					panic(fmt.Sprintf(
						"%v\nMessage: %s\n%#v\n",
						ErrAssertionFailure,
						msg,
						v,
					))
				}
			}

			if v.Map != nil {
				v.Value[i] = v.Map(a)
			}
		}
	}
}

func (parser *Parser) process() {
	parser.extract()
	parser.process_switches()
	parser.process_positional()
}

func (parser *Parser) Parse() *Parser {
	parser.process()
	parser.Switches = switches
	parser.Positionals = positionals
	return parser
}

func (parser *Parser) to_map() map[string][]string {
	out := map[string][]string{}

	for _, v := range parser.Switches {
		out[v.Name] = v.Value
	}

	for _, v := range parser.Positionals {
		out[v.Name] = []string{v.Value}
	}

	return out
}

func (parser *Parser) ParseMap() map[string][]string {
	parser.process()
	parser.Switches = switches
	parser.Positionals = positionals
  return parser.to_map()
}

func (S *Switch) header() string {
	var header string
	return header
}

func (parser *Parser) header() string {
	var header string
	return header
}

func (S *Switch) String(summary bool) string {
	var res string
	return res
}

func (parser *Parser) String(summary bool) string {
	var res string
	return res
}

func (parser *Parser) Get(key string) []string {
	x, ok := parser.Switches[key]
	if !ok {
		X, OK := parser.Positionals[key]
		if OK {
			return []string{X.Value}
		}
		return nil
	}
	return x.Value
}

func (parser *Parser) At(key int) (string, bool) {
	x, ok := parser.Positionals[strconv.Itoa(key)]
	if !ok {
		if key > -1 && key < len(parser.Argv) {
			return parser.Argv[key], true
		}
		return "", false
	}
	return x.Value, true
}

//////////////////////////////////////////////////
func main() {
	parser := NewParser([]string{
		"11",
		"-A", "1", "2", "3",
		"--a-switch", "4", "5", "6",
		"-B",
		"a",
		"b",
		"--",
		"1",
		"2",
		"3",
	}, true)

	parser.Switch(
		Names{"A", "a-switch"},
		&Switch{
			Nargs:           "+",
			AllowDuplicates: true,
		},
	)

	parser.Switch(
		Names{"B", "b-switch"},
		&Switch{N: 1},
	)

	parser.Positional("X", &Positional{})

  res := parser.ParseMap()
  for name, v := range res {
    fmt.Printf("%s: %#v\n", name, v)
  }
}
