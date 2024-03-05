package main

import (
    "errors"
    "flag"
    "fmt"
    "regexp"
    "slices"
    "strings"
)

type Switch struct {
    Argv            []string
    Name            string
    Names           []string
    Args            []string
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
}

type switch_ref struct {
    ref      *Switch
    pos      int
    args     []string
}

type switches map[string]*Switch

type Parser struct {
    Argv       []string
    Desc       string
    Header     string
    Footer     string
    Switches   switches
    Positional []string
    tail_argv  []string
    head_argv  []string
}

//////////////////////////////////////////////////
var ErrMissingName = errors.New("expected short and/or long name")
var ErrNoArgs = errors.New("no arguments passed")
var ErrExcessArgs = errors.New("excess arguments passed")
var ErrLessArgs = errors.New("not enough arguments passed")
var ErrDuplicate = errors.New("cannot pass this switch more than once")
var ErrInvalidNargs = errors.New("need any of +, ?, *, <int>")
var ErrAssertionFailure = errors.New("assertion failed")
var ErrInvalidChoice = errors.New("invalid choice")
var ErrMissingDeps = errors.New("missing dependencies")
var ErrUnallowedDeps = errors.New("unallowed dependencies passed")

//////////////////////////////////////////////////
var num_re = regexp.MustCompile("^[0-9]+$")
var nargs_re = regexp.MustCompile("^[+*?]+$")
var end_of_args_re = regexp.MustCompile("^--$")
var parsed = map[string]*switch_ref{}
var parsed_slices = []*switch_ref{}
var printf = fmt.Printf

func errf(err error, obj *Switch) {
    panic(fmt.Sprintf("%v\nSwitch: %#v\n", err, obj))
}

//////////////////////////////////////////////////
func NewParser(argv []string) *Parser {
    if argv == nil {
        argv = flag.Args()
    }

    var tail_argv []string = nil
    double_dash := slices.Index(argv, "--")
    if double_dash != -1 {
        tail_argv = argv[double_dash+1:]
        argv = argv[:double_dash]
    }

    return &Parser{
        Argv:      argv,
        Switches:  switches{},
        tail_argv: tail_argv,
    }
}

func (parser *Parser) Add(short string, long string, opts *Switch) *Parser {
    names := []string{short, long}
    if (len(names) == 0) || ((names[0] == "") && (names[1] == "")) {
        errf(ErrMissingName, opts)
    } else if names[1] != "" {
        opts.Name = names[1]
    } else if names[0] != "" {
        opts.Name = names[0]
    }

    opts.Names = names
    nargs := &opts.Nargs

    if *nargs != "" {
        if nargs_re.FindStringIndex(*nargs) == nil {
            errf(ErrInvalidNargs, opts)
        }
        opts.N = -1
    }

    if *nargs == "?" || opts.N == 0 || *nargs == "*" {
        opts.Optional = true
    }

    opts.Argv = parser.Argv
    parser.Switches[opts.Name] = opts
    opts.Find()

    return parser
}

func (S *Switch) Find() []int {
    argv := S.Argv
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
            pos = append(pos, i)
            short_matched = true
        }

        if !short_matched && matches("--", names[1], v) {
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

func (parser *Parser) Extract() *Parser {
    slices.SortFunc(
        parsed_slices,
        func(a *switch_ref, b *switch_ref) int {
            if a.pos < b.pos {
                return 1
            }
            return 0
        })

    parsed_slices_l := len(parsed_slices)
    first := parsed_slices[0]
    last := parsed_slices[parsed_slices_l-1]
    argv := parser.Argv
    l := len(argv)
    validate_n := func(S *Switch) {
        n := S.N
        nargs := S.Nargs
        args := S.Args
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
        parser.head_argv = argv[:first.pos]
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
        ref.Args = append(ref.Args, v.args...)
    }

    for _, v := range parser.Switches {
        if !v.Found || v == last.ref {
            continue
        }
        validate_n(v)
    }

    last_args := last.args
    last_gotten := len(last_args)
    last_nargs := last.ref.Nargs
    last_n := last.ref.N
    last.ref.Args = last.args

    if last_n != -1 {
        if last_gotten == 0 && last_n != 0 {
            errf(ErrLessArgs, last.ref)
        } else if last_n > last_gotten {
            errf(ErrLessArgs, last.ref)
        } else {
            last_args = argv[last.pos+1 : last.pos+last_gotten]
            parser.tail_argv = argv[last.pos+last_gotten:]
            last.ref.Args = last_args
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

  parser.Positional = append(parser.Positional, parser.head_argv...)
  parser.Positional = append(parser.Positional, parser.tail_argv...)

    return parser
}

func (parser *Parser) Process() *Parser {
    for _, v := range parser.Switches {
    deps := v.Requires
    excludes := v.Excludes

    if excludes != nil {
      for _, d := range excludes {
        _, ok := parser.Switches[d]
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
        _, ok := parser.Switches[d]
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

        for i, a := range v.Args {
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
            } else if ok, msg := v.Assert(a); !ok {
                panic(fmt.Sprintf(
                    "%v\nMessage: %s\n%#v\n",
                    ErrAssertionFailure,
                    msg,
                    v,
                ))
            }

            if v.Map != nil {
                v.Args[i] = v.Map(a)
            }
        }
    }

    return parser
}

func main() {
    parser := NewParser([]string{
        "-A", "1", "2", "3",
        "--a-switch", "4", "5", "6",
        "-B",
        "a",
        "b",
        "--",
        "1",
        "2",
        "3",
    })

    parser.Add("A", "a-switch", &Switch{
        Nargs:           "+",
        AllowDuplicates: true,
    })

    parser.Add("B", "", &Switch{
        N: 1,
    })

    parser.Extract()

    for _, v := range parser.Switches {
        printf("%s %#v\n", v.Name, v.Args)
    }
}
