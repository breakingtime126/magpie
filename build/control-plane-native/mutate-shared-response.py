"""Restore the old mutable response-file mechanism in an isolated test checkout.

Keep the delayed-reader test and gate. Run only the named deterministic test;
it must compile and fail its delayed-native-child generation assertion.
Never use this mutation as the candidate implementation.
"""
import pathlib
import sys

path = pathlib.Path(sys.argv[1])
source = path.read_text()
old = 'return filepath.Join(home, fmt.Sprintf("fixture-experimental-feature-response-%d", number))'
new = 'return filepath.Join(home, "fixture-experimental-feature-response")'
assert source.count(old) == 1, "generation path anchor changed"
assert source.count('os.O_WRONLY|os.O_CREATE|os.O_EXCL') == 1, "publication anchor changed"
source = source.replace(old, new).replace('os.O_WRONLY|os.O_CREATE|os.O_EXCL', 'os.O_WRONLY|os.O_CREATE|os.O_TRUNC')
path.write_text(source)
