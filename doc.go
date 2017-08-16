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

// Package dimacscnf provides a parser for the simplified DIMACS cnf format.
// This input format is often used in SAT competations or anywhere else where
// cnf formulae are processed.
//
// A more detailed description can be found on http://www.satcompetition.org/2009/format-benchmarks2009.html
//
// The easiest way to use this package is to use ParseDimacs,
// ParseDimacsWithBounds or ParseDimacsCheck. They all will try to parse
// a cnf from a io.Reader and return the information to you.
// The only difference between them is that they do different checks
// (is the input valid in accordance with the problem line?).
//
// If you know only correct files should be parsed use ParseDimacs, if it is
// important that the specification in the problem line is correct use
// ParseDimacsWithBounds and if it's also important if each variable occurs
// only once in each clause (it is a set without repetitions) and l and ¬l are
// not allowed in the same clause use ParseDimacsCheck.
//
// These three methods all return the clauses a [][]int, problem 'name'
// (usually "cnf") and nbvar from the problem line.
//
// If you want to create the clauses in a different way you can do that as well:
// Implement DimacsParserHandler and then use ParseGenericDimacs.
package dimacscnf
