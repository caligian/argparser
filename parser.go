package main

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

type Option struct {
	Name            string
	ShortName       string
	LongName        string
	Nargs           string
	N               int
	Assert          func(s string) error
	Metavar         string
	Help            string
	Map             func(s string) string
	Requires        []string
	Excludes        []string
	Required        bool
	Enum            []string
	AllowDuplicates bool
}

type argument struct {
	name  string
	value string
	opts  *Option
}

type keyword struct {
	name  string
	pos   int
  allPos []int
	value string
	opts  *Option
}

type Parser struct {
	Argv       []string
	Desc       string
	Header     string
	Footer     string
	ExitOnHelp bool
	Parsed     map[string][]string
}

//////////////////////////////////////////////////
var ErrMissingName = errors.New("expected short and/or long name")
var ErrNoArgs = errors.New("no arguments passed")
var ErrExcessArgs = errors.New("excess arguments passed")
var ErrLessArgs = errors.New("not enough arguments passed")
var ErrLessPosArgs = errors.New("not enough positional arguments passed")
var ErrDuplicate = errors.New("cannot pass this switch more than once")
var ErrInvalidNargs = errors.New("need any of +, ?, *, <int>")
var ErrAssertionFailure = errors.New("assertion failed")
var ErrInvalidChoice = errors.New("invalid choice")
var ErrMissingDeps = errors.New("missing dependencies")
var ErrUnallowedDeps = errors.New("unallowed dependencies passed")
var ErrNameConflict = errors.New("cannot use the same name for positional args and switches")

//////////////////////////////////////////////////
var numRe = regexp.MustCompile("^[0-9]+$")
var nargsRe = regexp.MustCompile("^[+*?]+$")
var headArgv = []string{}
var tailArgv = []string{}
var allArgv = []string{}
var argumentsMap = map[string]*argument{}
var keywordsMap = map[string]*keyword{}
var argumentsSlice = []*argument{}
var keywordsSlice = []*keyword{}
var parsedMap = map[string][]string{}
var checkDups = map[string]bool{}

//////////////////////////////////////////////////
func New(argv []string, exitOnHelp bool) *Parser {
	if argv == nil {
		argv = os.Args
	}

	eof := slices.Index(argv, "--")
	if eof != -1 {
		tailArgv = argv[eof+1:]
		argv = argv[:eof]
	}

	parser := &Parser{
		Argv:       argv,
		ExitOnHelp: exitOnHelp,
	}

	parser.Keyword(
		"h", "help",
		&Option{Help: "show this help"},
	)

	return parser
}

func (parser *Parser) Argument(name string, opts *Option) *Parser {
	opts.Name = name

	if name == "" {
		panic(fmt.Errorf("%w\nParser: %#v\n", ErrMissingName, parser))
	}

	if _, ok := argumentsMap[name]; ok {
		panic(fmt.Errorf("%w\nOption: %#v\n", ErrNameConflict, opts))
	}

	if _, ok := keywordsMap[name]; ok {
		panic(fmt.Errorf("%w\nOption: %#v\n", ErrNameConflict, opts))
	}

	argumentsMap[opts.Name] = &argument{
		name:  opts.Name,
		value: "",
		opts:  opts,
	}

	argumentsSlice = append(argumentsSlice, argumentsMap[opts.Name])

	return parser
}

func (parser *Parser) Keyword(short, long string, opts *Option) *Parser {
	if (short == "") && (long == "") {
		panic(fmt.Errorf("%w\nParser: %#v\n", ErrMissingName, parser))
	}

	if long != "" {
		opts.LongName = long
		opts.Name = long
	}

	if short != "" {
		opts.ShortName = short
    if opts.Name == "" {
      opts.Name = short
    }
	}

	if _, ok := argumentsMap[opts.Name]; ok {
		panic(fmt.Errorf("%w\nOption: %#v\n", ErrNameConflict, opts))
	}

	if _, ok := keywordsMap[opts.Name]; ok {
		panic(fmt.Errorf("%w\nOption: %#v\n", ErrNameConflict, opts))
	}

	nargs := &opts.Nargs
	if *nargs != "" {
		if nargsRe.FindStringIndex(*nargs) == nil {
			panic(fmt.Errorf("%w\nOption: %#v\n", ErrInvalidNargs, opts))
		}
		opts.N = -1
	}

	keywordsMap[opts.Name] = &keyword{
		name:  opts.Name,
		pos:   -1,
		value: "",
		opts:  opts,
	}

	return parser
}

func (parser *Parser) Find() {
	exitOnHelp := parser.ExitOnHelp
	argv := parser.Argv

	matches := func(prefix string, a string, b string) bool {
		a = strings.Join([]string{prefix, a}, "")
		return a == b
	}

  find := func(x *keyword) {
    opts := x.opts
    dup := opts.AllowDuplicates
    req := opts.Required

    for i, v := range argv {
      matched := -1

      if opts.ShortName != "" && matches("-", opts.ShortName, v) {
        if v == "-h" && exitOnHelp {
          os.Exit(0)
        }
        matched = i
      }

      if matched == -1 && matches("--", opts.LongName, v) {
        if v == "--help" && exitOnHelp {
          os.Exit(0)
        }
        matched = i
      }

      if matched == -1 && req {
        panic(fmt.Errorf("%w\nkeyword arg: %#v\n", ErrNoArgs, x))
      }

      if matched != -1 {
        y := *x
        y.pos = i
        keywordsSlice = append(keywordsSlice, &y)
        if checkDups[opts.Name] && !dup {
          panic(fmt.Errorf("%w\nkeyword arg: %#v\n", ErrDuplicate, x))
        } else {
          checkDups[opts.Name] = true
        }
      }
    }
  }

	for _, v := range keywordsMap {
		find(v)
	}

	slices.SortFunc(keywordsSlice, func(a, b *keyword) int {
		if a.pos < b.pos {
			return -1
		}
		return 1
	})
}

func (parser *Parser) Extract() {
	argv := parser.Argv
	first := keywordsSlice[0]
	keywordsL := len(keywordsSlice)
	last := keywordsSlice[keywordsL-1]

  if first.pos != 0 {
		headArgv = argv[:first.pos]
	}

	for i := 0; i < keywordsL-1; i++ {
		current := keywordsSlice[i]
		next := keywordsSlice[i+1]

		if _, ok := parsedMap[current.name]; !ok {
			parsedMap[current.name] = []string{}
		}

    res := append(parsedMap[current.name], argv[current.pos+1:next.pos]...)
		parsedMap[current.name] = res
	}

	parsedMap[last.name] = argv[last.pos+1:]
	lastArgs := parsedMap[last.name]
	lastArgsL := len(lastArgs)
	lastNargs := last.opts.Nargs
	lastN := last.opts.N

	if lastN != -1 {
		if lastArgsL > lastN {
			parsedMap[last.name] = argv[last.pos+1 : last.pos+lastN+1]
			tailArgv = append(argv[last.pos+lastN:], tailArgv...)
		} else if lastN == 0 {
			if lastArgsL > 0 {
				panic(fmt.Errorf("%w\nswitch: %#v\n", ErrExcessArgs, last.opts))
			}
		} else if lastN > lastArgsL {
			panic(fmt.Errorf("%w\nswitch: %#v\n", ErrLessArgs, last.opts))
		}
	} else {
		switch lastNargs {
		case "+":
			if lastArgsL == 0 {
				panic(fmt.Errorf("%w\nswitch: %#v\n", ErrLessArgs, last.opts))
			}
		case "?":
			if lastArgsL > 1 {
				panic(fmt.Errorf("%w\nswitch: %#v\n", ErrExcessArgs, last.opts))
			}
		}
	}

	allArgv = append(headArgv, tailArgv...)
	allArgvL := len(allArgv)
	argumentsSliceL := len(argumentsSlice)

	if allArgvL < argumentsSliceL {
		panic(fmt.Errorf("%w\nreason: expected %d args, got %d\n", ErrLessArgs, argumentsSliceL, allArgvL))
	}

	for i, v := range argumentsSlice {
    res := []string{allArgv[i]}
		parsedMap[v.name] = res
		parsedMap[strconv.Itoa(i)] = res
	}

	for i := argumentsSliceL; i < allArgvL; i++ {
		name := strconv.Itoa(i)
		parsedMap[name] = []string{argv[i]}
	}
}

func (parser *Parser) Validate() {
	last := keywordsSlice[len(keywordsSlice)-1]

	checkAssert := func(name, nameType string, assert func(s string) error, xs []string) {
		if assert == nil {
			return
		}

		for _, x := range xs {
			if err := assert(x); err != nil {
				panic(fmt.Errorf(
					"%w\nAssertion failure for %s [%s]\n",
					ErrAssertionFailure,
					name,
					nameType,
				))
			}
		}
	}

	checkEnum := func(name, nameType string, enum, xs []string) {
		if enum == nil {
			return
		}

		for _, x := range xs {
			if slices.Index(enum, x) == -1 {
				panic(fmt.Sprintf(
          "%v\nChoices: %s\nGiven: %s\n%s [%s]\n",
					ErrInvalidChoice,
					strings.Join(enum, ","),
					strings.Join(xs, ","),
					name,
					nameType,
				))
			}
		}
	}

	for name, args := range parsedMap {
		if name == last.name {
			continue
		}

		var keywordX *keyword
		var argX *argument

		if x, ok := keywordsMap[name]; ok {
			keywordX = x
		} else if x, ok := argumentsMap[name]; ok {
			argX = x
		}

		if keywordX != nil {
			opts := keywordX.opts
			n := opts.N
			nargs := opts.Nargs
			gotten := len(args)

			if (n == 0 || nargs == "?" || nargs == "*") && gotten == 0 {
				return
			} else if n != -1 {
				if n > gotten {
					panic(fmt.Errorf("%w\nswitch: %#v\n", ErrLessArgs, opts))
				} else if n < gotten {
					panic(fmt.Errorf("%w\nswitch: %#v\n", ErrExcessArgs, opts))
				}
				return
			}

			switch nargs {
			case "+":
				if gotten == 0 {
					panic(fmt.Errorf("%w\nswitch: %#v\n", ErrLessArgs, opts))
				}
			case "?":
				if gotten > 1 {
					panic(fmt.Errorf("%w\nswitch: %#v\n", ErrExcessArgs, opts))
				}
			}

			checkEnum(name, "keyword", opts.Enum, args)
			checkAssert(name, "keyword", opts.Assert, args)

			if opts.Map != nil {
				for i, v := range args {
					parsedMap[name][i] = opts.Map(v)
				}
			}
		}

    argx, ok := argumentsMap[name]
    if !ok {
      continue
    }

    argX = argx
		opts := argX.opts
		checkEnum(name, "argument", argX.opts.Enum, args)
		checkAssert(name, "argument", argX.opts.Assert, args)

		if opts.Map != nil {
			for i, v := range args {
				parsedMap[name][i] = opts.Map(v)
			}
		}
	}
}

func (parser *Parser) Parse() map[string][]string {
	parser.Find()
	parser.Extract()
	parser.Validate()
	parser.Parsed = parsedMap

	return parsedMap
}

func sentenceLen(x []string) int {
	n := 0
	for _, v := range x {
		n += len(v)
	}
	return n
}

func (S *keyword) genHeader() []string {
	opts := S.opts
	mvar := opts.Metavar
	header := []string{}
	short := opts.ShortName
	long := opts.LongName
	nargs := opts.Nargs
	n := opts.N

	push := func(s string) {
		header = append(header, s)
	}

	if short != "" {
		push(short)
	} else {
		push(long)
	}

	if mvar == "" {
		if short != "" {
			mvar = strings.ToUpper(short)
		} else {
			mvar = "STR"
		}
	}

	if nargs != "" {
		switch nargs {
		case "?":
			push(fmt.Sprintf("[%s]", mvar))
		case "*":
			push(fmt.Sprintf("[%s, ...]?", mvar))
		case "+":
			push(fmt.Sprintf("{%s, ...}", mvar))
		}
	} else if n > 0 {
		push(fmt.Sprintf("%s{%d}", mvar, n))
	}

	return header
}

func (parser *Parser) genHeader() string {
	var header string
	return header
}

func (S *argument) genHelp(summary bool) string {
	var res string
	return res
}

func (parser *Parser) genHelp(summary bool) string {
	var res string
	return res
}

//////////////////////////////////////////////////
func main() {
	parser := New([]string{
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

	parser.Keyword(
		"A", "a-switch",
		&Option{
			Nargs:           "+",
			AllowDuplicates: true,
      Enum: []string{"1", "2", "3", "4", "5", "6"},
		},
	)

	parser.Keyword(
		"B", "b-switch",
		&Option{N: 1, AllowDuplicates: true},
	)

	parser.Argument("X", &Option{})

	res := parser.Parse()
	for name, v := range res {
		fmt.Printf("%s: %#v\n", name, v)
	}
}
