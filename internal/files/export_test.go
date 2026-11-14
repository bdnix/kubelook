package files

// Open exposes the channel opener to the black-box tests.
var Open = open

// ValidateTarHeader exposes the archive entry check to the table test.
var ValidateTarHeader = validateTarHeader

// ErrUnsafe exposes the sentinel.
var ErrUnsafe = ErrUnsafeArchive
