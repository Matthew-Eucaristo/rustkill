# Homebrew formula for Matthew-Eucaristo/homebrew-tap (Formula/rustkill.rb).
# Builds from the tagged source tarball. Bump `url` and `sha256` per release;
# see RELEASING.md.
class Rustkill < Formula
  desc "Reclaim disk space from Rust target folders, Cargo caches and toolchains"
  homepage "https://github.com/Matthew-Eucaristo/rustkill"
  url "https://github.com/Matthew-Eucaristo/rustkill/archive/refs/tags/v0.1.0.tar.gz"
  sha256 "REPLACE_WITH_SHA256_OF_THE_TAG_TARBALL"
  license "MIT"
  head "https://github.com/Matthew-Eucaristo/rustkill.git", branch: "main"

  depends_on "go" => :build

  def install
    system "go", "build", *std_go_args(ldflags: "-X main.version=#{version}")
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/rustkill --version")
    assert_match "\"schema_version\": 1", shell_output("#{bin}/rustkill capabilities --json")

    (testpath/"dev/app/Cargo.toml").write "[package]\nname = \"app\"\nversion = \"0.1.0\"\n"
    (testpath/"dev/app/target/CACHEDIR.TAG").write <<~TAG
      Signature: 8a477f597d28d172789f06886806bc55
      # This file is a cache directory tag created by cargo.
    TAG
    (testpath/"dev/app/target/.rustc_info.json").write "{}"
    output = shell_output("#{bin}/rustkill scan --json --no-global #{testpath}/dev")
    assert_match "\"kind\": \"target\"", output
    assert_path_exists testpath/"dev/app/target" # scan never changes anything
  end
end
