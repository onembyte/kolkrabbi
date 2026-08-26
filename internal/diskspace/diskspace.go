// Package diskspace answers how much room is left where Kolkrabbi wants to
// write. It is a platform package because the answer comes from a syscall that
// differs per OS, and an adapter must not carry that divergence itself.
//
// Every implementation reports "unknown" rather than a number it could not
// measure. Callers refuse on unknown, so a confident zero or a guessed total
// would be worse than no answer at all.
package diskspace
