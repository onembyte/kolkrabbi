package protocol

// Version mirrors spec/VERSION. It stays a string because it is a wire value,
// not a number a client performs arithmetic on and not a product SemVer tag.
const Version = "0"
