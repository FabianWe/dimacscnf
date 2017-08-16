// The MIT License (MIT)
//
// Copyright (c) 2017 Fabian Wenzelmann
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package dimacscnf

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"unicode"
	"unicode/utf8"
)

// iterativeAverage computes iteratively the average of a series of values.
// Implemented as described here: http://people.revoledu.com/kardi/tutorial/RecursiveStatistic/Time-Average.htm
// In contrast to the method described above t starts with 0, not 1.
// So to compute the average of a series do:
// 1. Initialize your current average to whatever you want
// 2. Initialize t = 0
// 3. For each sample update current = iterativeAverage(t, nextSample, current)
//    and increase t by 1.
func iterativeAverage(t int, value, current float64) float64 {
	return (float64(t)/float64(t+1))*current + (1.0/float64(t+1))*value
}

// dimacsSplit stores some information that we need for the split function
// (gives some context and memory to this function).
// The main purpose is to have a bufio.SplitFunc with some memory (what was
// the last delimeter that ended a word etc.)
type dimacsSplit struct {
	currentDelimeter rune // The delimeter found in the last split call
	lastDelimeter    rune // The delimeter found in the split call before the last one.
	atEOF            bool // True if in the last split call EOF was reached
	lineNumber       int  // The line number, gets increased with each newline
}

// newDIMACSSplit returns a new newDIMACSSplit.
func newDIMACSSplit() *dimacsSplit {
	return &dimacsSplit{atEOF: false,
		lineNumber: 1}
}

// split is a bufio.SplitFunc that sets the fields of the dimacsSplit accordingly.
// It was strongly influenced by bufio.ScanWords
func (split *dimacsSplit) split(data []byte, atEOF bool) (advance int, token []byte, err error) {
	// set lastDelimeter to the current delimeter before it gets updated
	split.lastDelimeter = split.currentDelimeter
	// Skip all leading spaces expect new lines
	start := 0
	var width int
	for ; start < len(data); start += width {
		var r rune
		r, width = utf8.DecodeRune(data[start:])
		if unicode.IsSpace(r) {
			// if it is a space: check if it is a new line, in this case
			// return the empty string and move after the new line
			if r == '\n' {
				split.currentDelimeter = '\n'
				split.lineNumber++
				return start + width, []byte(""), nil
			}
			// otherwise just ignore it
		} else {
			// if r is not a space break the loop
			break
		}
	}
	// Scan until space, marking end of word.
	for i := start; i < len(data); i += width {
		var r rune
		r, width = utf8.DecodeRune(data[i:])
		if unicode.IsSpace(r) {
			if r == '\n' {
				split.lineNumber++
			}
			split.currentDelimeter = r
			return i + width, data[start:i], nil
		}
	}
	// If we're at EOF, we have a final, non-empty, non-terminated word. Return it.
	if atEOF && len(data) > start {
		split.atEOF = true
		// make sure delimeter is not set to \n
		split.currentDelimeter = ' '
		return len(data), data[start:], nil
	}
	// Request more data.
	return 0, nil, nil
}

// DimacsParserHandler provides method to receive commands from the parser
// when a new clause or variable is encountered.
//
// The method ProblemLine is called once with the problem specification,
// where problem is the name of the problem (usually "cnf"), nbvar is the number
// of variables and nbclauses the number of clauses.
//
// NewClause gets called for each new clause in the file, NewVariable for each
// variable (so NewClause should be called before).
//
// Done gets called once everything is done.
//
// The parsing procedure will stop if one of the methods return an error.
type DimacsParserHandler interface {
	ProblemLine(problem string, nbvar, nbclauses int) error
	NewClause() error
	NewVariable(value int) error
	Done() error
}

// ParseError is a special error that wraps another error and also stores the
// line number from the input file.
type ParseError struct {
	err        error
	lineNumber int
}

// NewParseError returns a new ParseError.
func NewParseError(err error, lineNumber int) ParseError {
	return ParseError{err: err,
		lineNumber: lineNumber}
}

func (err ParseError) Error() string {
	return fmt.Sprintf("Error in line %d: %s", err.lineNumber, err.err.Error())
}

// defaultHandler is a DimacsParserHandler that simply accepts input and does
// not check if this input is valid (according to the problem line).
type defaultHandler struct {
	clauses   [][]int // Stores the clauses found so far.
	problem   string  // The problem "name" from the problem line.
	nbvar     int     // Number of variables from the problem line.
	nbclauses int     // Number of clauses from the problem line.

	newClauseSize int     // < 0: Use average, otherwise use this value as new clause capacity.
	current       float64 // Current average value, gets adjusted with new clauses.
}

func newDefaultHandler(newClauseSize int) *defaultHandler {
	return &defaultHandler{clauses: nil,
		problem:       "",
		nbvar:         -1,
		nbclauses:     -1,
		newClauseSize: newClauseSize,
		current:       0.0}
}

// iterateAverage iterates the current average value taking in account the
// length of the last clause (if such a clause exists).
func (h *defaultHandler) iterateAverage() {
	if len(h.clauses) == 0 {
		return
	}
	h.current = iterativeAverage(len(h.clauses)-1,
		float64(len(h.clauses[len(h.clauses)-1])),
		h.current)
}

// getClauseSize returns the initial capacity of a new clause.
// If newClauseSize is >= 0 this value is returned, otherwise the
// current average value is returned.
func (h *defaultHandler) getClauseSize() int {
	if h.newClauseSize >= 0 {
		return h.newClauseSize
	} else {
		// convert h.current to int
		return int(math.Max(h.current, 0.0))
	}
}

func (h *defaultHandler) ProblemLine(problem string, nbvar, nbclauses int) error {
	h.clauses, h.problem, h.nbvar, h.nbclauses = make([][]int, 0, nbclauses), problem, nbvar, nbclauses
	return nil
}

func (h *defaultHandler) NewClause() error {
	h.iterateAverage()
	capacity := h.getClauseSize()
	h.clauses = append(h.clauses, make([]int, 0, capacity))
	return nil
}

func (h *defaultHandler) NewVariable(value int) error {
	if len(h.clauses) == 0 {
		return errors.New("Trying to add a variable but no clause was created, parser error?")
	}
	i := len(h.clauses) - 1
	h.clauses[i] = append(h.clauses[i], value)
	return nil
}

func (h *defaultHandler) Done() error {
	return nil
}

// boundaryHandler is a DimacsParserHandler that also checks if the bounds in
// the problem line are met: This is if exactly the right number of clauses
// were parsed and if all variables are valid (i.e. in the range of nbvar).
type boundaryHandler struct {
	*defaultHandler
	currentClauseOccurrences map[int]struct{}
}

func newBoundaryHandler(newClauseSize int) *boundaryHandler {
	return &boundaryHandler{newDefaultHandler(newClauseSize),
		nil}
}

func (h *boundaryHandler) NewClause() error {
	// first check if we already reached the number of clauses
	if len(h.clauses) >= h.nbclauses {
		return fmt.Errorf("Expected to parse %d clauses but more were found", h.nbclauses)
	}
	return h.defaultHandler.NewClause()
}

func (h *boundaryHandler) NewVariable(value int) error {
	abs := value
	if abs < 0 {
		abs *= -1
	}
	// check boundary
	if abs > h.nbvar {
		return fmt.Errorf("nbvar in problem line is %d, but got variable %d", h.nbvar, value)
	}
	return h.defaultHandler.NewVariable(value)
}

func (h *boundaryHandler) Done() error {
	// check if all clauses were found
	if len(h.clauses) != h.nbclauses {
		return fmt.Errorf("Expected %d clauses but found %d", h.nbclauses, len(h.clauses))
	}
	return h.defaultHandler.Done()
}

// checkHandler is a DimacsParserHandler that will do the same tests as the
// boundaryHandler but will also check if no variable occurs more than once in
// each clause. That is the literal l must not occur twice in the clause,
// also l and ¬l are not allowed in the same clause.
// For this another set must be stored, so this consumes more memory.
type checkHandler struct {
	*boundaryHandler
}

func newCheckHandler(newClauseSize int) *checkHandler {
	return &checkHandler{newBoundaryHandler(newClauseSize)}
}

func (h *checkHandler) NewClause() error {
	// create a new map for the new clause
	capacity := h.getClauseSize()
	h.currentClauseOccurrences = make(map[int]struct{}, capacity)
	return h.boundaryHandler.NewClause()
}

func (h *checkHandler) NewVariable(value int) error {
	// check if variable appeared already (positive or negative)
	_, hasValue := h.currentClauseOccurrences[value]
	if hasValue {
		return fmt.Errorf("Variable %d occurs multiple times in clause", value)
	}
	_, hasNegValue := h.currentClauseOccurrences[-value]
	if hasNegValue {
		return fmt.Errorf("Trying to insert %d, but %d was inserted before", value, -value)
	}
	// now everything is ok, add occurrence
	h.currentClauseOccurrences[value] = struct{}{}
	return h.boundaryHandler.NewVariable(value)
}

func (h *checkHandler) Done() error {
	// just to release some memory, should not be wrong anyway :)
	h.currentClauseOccurrences = nil
	return h.boundaryHandler.Done()
}

// parserState stores the state the dimacs parser is in right now
// (state machine).
type parserState int

const (
	header           parserState = iota // In the header, only lines with c and p are accepted.
	inComment                           // We're inside a comment so skip everything until a newline is found.
	problemName                         // Read the next word as the problem name.
	problemnbvar                        // Read the next word as nbvar.
	problemnbclauses                    // Read the next word as nbclauses.
	clause                              // Read a clause (must end with 0).
	inClause                            // Read values inside a clause until 0 is found.
)

func (state parserState) String() string {
	switch state {
	case header:
		return "header"
	case inComment:
		return "inComment"
	case problemName:
		return "problemName"
	case problemnbvar:
		return "problemnbvar"
	case problemnbclauses:
		return "problemnbclauses"
	case clause:
		return "clause"
	case inClause:
		return "inClause"
	default:
		return "INVALID"
	}
}

// parseConfiguration stores the state the parser is in as well as the
// information from the problem line.
type parseConfiguration struct {
	state     parserState
	name      string
	nbvar     int
	nbclauses int
}

func newParseConfiguration() *parseConfiguration {
	return &parseConfiguration{state: header,
		nbvar:     -1,
		nbclauses: -1}
}

// parseClauseEntry tries to read the token as a number, if that number is zero
// return the clause state, otherwise remain in the inClause state.
func (c *parseConfiguration) parseClauseEntry(h DimacsParserHandler,
	token string,
	lastDelimeter,
	delimeter rune) (parserState, error) {
	// parse number
	num, parseErr := strconv.Atoi(token)
	if parseErr != nil {
		return -1, parseErr
	}
	// if number is zero switch back to the state where we expect new clauses
	if num == 0 {
		return clause, nil
	}
	// it is not allowed to end this variable with a new line
	if delimeter == '\n' {
		return -1, errors.New("Not finished clause ended with a new line")
	}
	// otherwise add the variable
	addErr := h.NewVariable(num)
	if addErr != nil {
		return -1, addErr
	}
	// now everything is ok, go on to the next variable
	return inClause, nil
}

// nextState determines the next state (sets the state of the configuration)
// depending on the current state, the next input token, the delimeter
// that ended the current taken and the lastDelimeter that ended the token
// in the previous call.
func (c *parseConfiguration) nextState(h DimacsParserHandler,
	token string,
	lastDelimeter,
	delimeter rune) error {
	switch c.state {
	case header:
		// if we are in the header of the file we must read a c or p
		switch token {
		case "c":
			// now we switch to the in comment state to read the whole line as a comment
			c.state = inComment
			return nil
		case "p":
			// next we must read the problem name
			c.state = problemName
			return nil
		default:
			return fmt.Errorf("Expected token 'p' or 'c' (header of the file). Got %s", token)
		}
	case inComment:
		// if the token that delmited the next word is newline we return to the
		// header mode, otherwise stay in this mode
		if delimeter == '\n' {
			c.state = header
			return nil
		} else {
			c.state = inComment
			return nil
		}
	case problemName:
		// now we must read the "name", for example cnf
		// also we check if the delimeter before was not new line (invalid syntax)
		if lastDelimeter == '\n' {
			return errors.New("problem line was interrupted by a newline")
		} else {
			// it's okay, we got the name, so now we must read problemnbvar
			c.name = token
			c.state = problemnbvar
			return nil
		}
	case problemnbvar:
		// again we check the lastDelimeter
		if lastDelimeter == '\n' {
			return errors.New("problem line was interrupted by a newline")
		} else {
			// ok, assign variable and wait for problemnbclauses
			var parseErr error
			c.nbvar, parseErr = strconv.Atoi(token)
			if parseErr != nil {
				return parseErr
			}
			if c.nbvar < 0 {
				return errors.New("nbvar must be >= 0")
			}
			c.state = problemnbclauses
			return nil
		}
	case problemnbclauses:
		// again...
		if lastDelimeter == '\n' {
			return errors.New("problem line was interrupted by a newline")
		} else {
			// now we finally read the complete problem line
			var parseErr error
			c.nbclauses, parseErr = strconv.Atoi(token)
			if parseErr != nil {
				return parseErr
			}
			if c.nbclauses < 0 {
				return errors.New("nbclauses must be >= 0")
			}
			// finally we can apply the problem function
			if problemErr := h.ProblemLine(c.name, c.nbvar, c.nbclauses); problemErr != nil {
				return problemErr
			}
			c.state = clause
			return nil
		}
	case clause:
		// if we are in this state we expect a new clause
		// we ignore empty lines containing nothing
		// and possible whitespaces (though this should never happen)
		if token == "" {
			return nil
		} else {
			// a new clause must be created
			if clauseErr := h.NewClause(); clauseErr != nil {
				return clauseErr
			}
			// check current token and process the first number
			nextState, entryErr := c.parseClauseEntry(h, token, lastDelimeter, delimeter)
			if entryErr != nil {
				return entryErr
			} else {
				c.state = nextState
				return nil
			}
		}
	case inClause:
		// in this state we simply add the variable to the current clause
		// if the token is empty the line was ended before it was allowed
		if token == "" {
			return errors.New("problem line was interrupted by a newline")
		}
		nextState, entryErr := c.parseClauseEntry(h, token, lastDelimeter, delimeter)
		if entryErr != nil {
			return entryErr
		} else {
			c.state = nextState
			return nil
		}
	default:
		return errors.New("Unkown parsing state")
	}
}

// ParseGenericDimacs tries to parse the content of the io.Reader as DIMACS
// and calls the handle methods of the DimacsParserHandler.
// If one of the handle methods return an error the parsing stops.
func ParseGenericDimacs(h DimacsParserHandler, r io.Reader) error {
	split := newDIMACSSplit()
	scanner := bufio.NewScanner(r)
	scanner.Split(split.split)
	parserConfig := newParseConfiguration()
	for scanner.Scan() {
		token := scanner.Text()
		stateErr := parserConfig.nextState(h, token, split.lastDelimeter, split.currentDelimeter)
		if stateErr != nil {
			return NewParseError(stateErr, split.lineNumber)
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	// check the last state, not all states are valid end states
	switch parserConfig.state {
	case header, problemName, problemnbvar, problemnbclauses:
		return NewParseError(errors.New("Unexpected end of input"), split.lineNumber)
	}
	return h.Done()
}

// ParseDimacs parses the contents of the io.Reader as DIMACS and returns
// the problem name (usually "cnf"), the number of variables and the clauses
// as a slice. If something with the syntax is wrong a ParseError is returned,
// otherwise the error from the underlying reader is returned.
// If the error is nil everything was ok.
//
// initialClauseSize may be specified to give the parser a hint about the
// size of clauses. That is when a new clause gets generated it will have
// this value as its initial capacity. Of course clauses can grow bigger, but
// less memory reallocation may be required.
// By setting it to a value >= 0 all clauses will have this clause size as
// the initial capacity.
// If it is set to a value < 0 it will compute the average clause size of
// clauses parsed so far and use this value as the initial clause size.
// This may help, but is not guaranteed to. If the length of the clauses differs
// very much too much or to less memory may be allocated.
func ParseDimacs(r io.Reader, initialClauseSize int) (string, int, [][]int, error) {
	h := newDefaultHandler(initialClauseSize)
	if err := ParseGenericDimacs(h, r); err != nil {
		return "", -1, nil, err
	}
	return h.problem, h.nbvar, h.clauses, nil
}

// ParseDimacsWithBounds does the same as ParseDimacs but also checks the
// bounds specified in the problem line.
// This is if exactly the right number of clauses were parsed and if all
// variables are valid (i.e. in the range of nbvar).
// It will however not check if all variables occur at least once somewhere
// in the formula (so specifying ten variables and using only two in the input
// would be ok).
func ParseDimacsWithBounds(r io.Reader, initialClauseSize int) (string, int, [][]int, error) {
	h := newBoundaryHandler(initialClauseSize)
	if err := ParseGenericDimacs(h, r); err != nil {
		return "", -1, nil, err
	}
	return h.problem, h.nbvar, h.clauses, nil
}

// ParseDimacsCheck does the same as ParseDimacsWithBounds but also checks if
// no variable occurs more than once in each clause. That is the literal l must
// not occur twice in the clause, also l and ¬l are not allowed in the same
// clause.
//
// For this a set must be stored, so this consumes more memory.
func ParseDimacsCheck(r io.Reader, initialClauseSize int) (string, int, [][]int, error) {
	h := newCheckHandler(initialClauseSize)
	if err := ParseGenericDimacs(h, r); err != nil {
		return "", -1, nil, err
	}
	return h.problem, h.nbvar, h.clauses, nil
}
