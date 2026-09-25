// Stamps one version into every SDK: node scripts/set-version.mjs 0.2.0
// (PHP and Go take their version from the git tag, so only their User-Agent constant changes.)
import fs from 'node:fs'

const version = process.argv[2]?.replace(/^v/, '')
if (!/^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$/.test(version ?? '')) {
  console.error('usage: node scripts/set-version.mjs <semver>')
  process.exit(1)
}

const edits = [
  ['node/package.json', /("version":\s*")[^"]+(")/],
  ['python/pyproject.toml', /(^version = ")[^"]+(")/m],
  ['python/src/relaya/__init__.py', /(__version__ = ")[^"]+(")/],
  ['php/src/Client.php', /(public const VERSION = ')[^']+(')/],
  ['go/version.go', /(const Version = ")[^"]+(")/],
  ['java/pom.xml', /(<artifactId>relaya-java<\/artifactId>\s*<version>)[^<]+(<\/version>)/],
  ['java/src/main/java/io/relaya/Relaya.java', /(public static final String VERSION = ")[^"]+(")/],
]

for (const [file, re] of edits) {
  const src = fs.readFileSync(file, 'utf8')
  if (!re.test(src)) throw new Error(`version not found in ${file}`)
  fs.writeFileSync(file, src.replace(re, `$1${version}$2`))
  console.log(`${file} -> ${version}`)
}
