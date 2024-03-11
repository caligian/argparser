package main

import (
	"errors"
	"fmt"
	"golang.org/x/term"
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
	value string
	opts  *Option
}

type Parser struct {
	Argv       []string
	Help       string
	ExitOnHelp bool
	Parsed     map[string][]string
	Summary    string
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
func getTermWidth() int {
	defaultwidth := 60
	if !term.IsTerminal(0) {
		return defaultwidth
	} else {
		width, _, err := term.GetSize(0)
		if err != nil {
			return defaultwidth
		}
		return width
	}
}

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
var termWidth = getTermWidth()
var textWidth = termWidth / 2

//////////////////////////////////////////////////
func New(argv []string) *Parser {
	if argv == nil {
		argv = os.Args
	}

	eof := slices.Index(argv, "--")
	if eof != -1 {
		tailArgv = argv[eof+1:]
		argv = argv[:eof]
	}

	parser := &Parser{
		Argv: argv,
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
		return (prefix + a) == b
	}

	find := func(x *keyword) {
		opts := x.opts
		dup := opts.AllowDuplicates
		req := opts.Required

		for i, v := range argv {
			matched := -1
			if opts.ShortName != "" && matches("-", opts.ShortName, v) {
				if v == "-h" && exitOnHelp {
					fmt.Println(parser.genHeader())
					os.Exit(0)
				}
				matched = i
			}

			if matched == -1 && matches("--", opts.LongName, v) {
				if v == "--help" && exitOnHelp {
					fmt.Println(parser.genHelp())
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

func (S *keyword) genHeader(useLong bool, addRequiredHint bool) string {
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
		if useLong && long != "" {
			push("-" + short + ", --" + long)
		} else {
			push("-" + short)
		}
	} else {
		push("--" + long)
	}

	if addRequiredHint && !opts.Required {
		header[len(header)-1] += "?"
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
			push(fmt.Sprintf("[%s,...]", mvar))
		case "+":
			push(fmt.Sprintf("{%s,...}", mvar))
		}
	} else if n > 0 {
		if n == 1 {
			push(fmt.Sprintf("{%s}", mvar))
		} else {
			push(fmt.Sprintf("{%s<%d>}", mvar, n))
		}
	}

	return strings.Join(header, " ")
}

func (x *argument) genHeader() string {
	mvar := x.opts.Metavar
	if mvar == "" {
		mvar = strings.ToUpper(x.name)
	}
	return fmt.Sprintf("%s", mvar)
}

func (parser *Parser) genHeader() string {
	scriptName := parser.Summary
	header := strings.Builder{}
	header.WriteString("Usage: ")
	header.WriteString(scriptName)
	header.WriteString(" ")
	scriptNameL := header.Len()
	ws := strings.Repeat(" ", scriptNameL)

	if scriptNameL > termWidth {
		header.WriteString("\n")
		ws = strings.Repeat(" ", textWidth)
		header.WriteString(ws)
	}

	totalLen := scriptNameL

	for _, v := range argumentsSlice {
		h := v.genHeader()
		hL := len(h)

		if totalLen >= termWidth || totalLen+hL >= termWidth {
			totalLen = 0
			header.WriteString("\n")
			header.WriteString(ws)
			header.WriteString(h)
			totalLen += scriptNameL
		} else {
			header.WriteString(h)
		}

		header.WriteString(" ")
		totalLen += hL + 1
	}

	for _, v := range keywordsMap {
		h := v.genHeader(false, false)
		hL := len(h)

		if totalLen >= termWidth || totalLen+hL >= termWidth {
			totalLen = 0
			header.WriteString("\n")
			header.WriteString(ws)
			header.WriteString(h)
			totalLen += scriptNameL
		} else {
			header.WriteString(h)
		}

		header.WriteString(" ")
		totalLen += hL + 1
	}

	return header.String()
}

func (S *argument) genHelp() string {
	res := strings.Builder{}
	header := S.genHeader()
	res.WriteString(header)
	headerL := len(header)
	r := textWidth / 3
	ws := strings.Repeat(" ", r)
	totalLen := r

	if r <= headerL {
		res.WriteString("\n")
		res.WriteString(ws)
	} else {
		res.WriteString(strings.Repeat(" ", r-headerL))
	}

	for _, v := range strings.Split(S.opts.Help, " ") {
		vL := len(v)

		if totalLen >= termWidth || totalLen+vL >= termWidth {
			totalLen = 0
			res.WriteString("\n")
			res.WriteString(ws)
			res.WriteString(v)
			totalLen += r
		} else {
			res.WriteString(v)
		}

		res.WriteString(" ")
		totalLen += vL + 1
	}

	return res.String()
}

func (S *keyword) genHelp() string {
	res := strings.Builder{}
	header := S.genHeader(true, true)
	res.WriteString(header)

	r := textWidth / 3
	ws := strings.Repeat(" ", r)
	totalLen := r
	headerL := len(header)

	if r <= headerL {
		res.WriteString("\n")
		res.WriteString(ws)
	} else {
		res.WriteString(strings.Repeat(" ", r-headerL))
	}

	for _, v := range strings.Split(S.opts.Help, " ") {
		vL := len(v)
		if totalLen >= termWidth || totalLen+vL >= termWidth {
			totalLen = 0
			res.WriteString("\n")
			res.WriteString(ws)
			res.WriteString(v)
			totalLen += r
		} else {
			res.WriteString(v)
		}

		res.WriteString(" ")
		totalLen += vL + 1
	}

	return res.String()
}

func (parser *Parser) genHelp() string {
	res := strings.Builder{}
	res.WriteString(parser.genHeader())
	res.WriteString("\n")
	totalLen := 0

	for _, v := range strings.Split(parser.Help, " ") {
		vL := len(v)
		if totalLen >= termWidth || totalLen+vL >= termWidth {
			totalLen = 0
			res.WriteString("\n")
			res.WriteString(v)
		} else {
			res.WriteString(v)
		}

		res.WriteString(" ")
		totalLen += vL + 1
	}

	res.WriteString("\n\nArguments:\n")
	for _, v := range argumentsMap {
		res.WriteString(v.genHelp())
		res.WriteString("\n")
	}

	res.WriteString("\nKeyword arguments:\n")
	for _, v := range keywordsMap {
		res.WriteString(v.genHelp())
		res.WriteString("\n")
	}

	return res.String()
}

//////////////////////////////////////////////////
func main() {
	parser := New([]string{
		// "--help",
		"11",
		"-a", "1", "2", "3",
		"--a-switch", "4", "5", "6",
		"-b",
		"a",
		"b",
		"--",
		"1",
		"2",
		"3",
	})

	parser.ExitOnHelp = true
	parser.Help = "this is an app that will fucking change your life for good you know!"
	parser.Summary = "bhangar ki shakal ke laude"

	parser.Keyword(
		"a", "a-switch",
		&Option{
			Nargs: "+",
			// N:               6,
			AllowDuplicates: true,
			Enum:            []string{"1", "2", "3", "4", "5", "6"},
			Help:            "this helps you to cure cancer",
			Required:        true,
		},
	)

	parser.Keyword(
		"b", "",
		&Option{Nargs: "*", AllowDuplicates: true, Help: "this helps you to take a huge shit"},
	)

	parser.Argument("x", &Option{
		Help: "this will help you to grow your dick size by over 99%!",
	})

	parser.Argument("y", &Option{
		Help: "this will help you to grow your left boob size by over 99%!",
	})

	// parse all the arguments
	// res := parser.Parse()
	// for name, v := range res {
	// 	fmt.Printf("%s: %#v\n",name, v)
	// }

	// fmt.Printf("%#v\n", res)
	//fmt.Printf("%#v\n", argumentsMap["X"].genHeader())
	// fmt.Printf("%s\n", keywordsMap["a-switch"].genHelp())
	println(parser.genHelp())
}
