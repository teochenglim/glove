# Homebrew formula for glove (v0.9.0, PRD §9).
#
# The tap repo (homebrew-<user>/…) is a maintainer action; this formula and
# its docs ship in-repo so the packaging is reviewable and test-asserted
# (TestAC_0_9_1_formula_matches_release_layout pins the URLs to the release
# workflow's artifact naming).
#
# Per-release maintenance (the part brew can't do for a hand-rolled tap):
#   scripts/update-homebrew-sha.sh <version>   # fills the sha256 lines from
#                                              # the release's checksums.txt
# Then copy this file into the tap as Formula/glove.rb.
class Glove < Formula
  desc "Local guard for AI agent tool calls — allow / review / block before execution"
  homepage "https://github.com/teochenglim/glove"
  version "0.9.0"
  license "Apache-2.0"

  depends_on macos: :el_capitan
  # linux is fine natively (glibc static-ish CGO_ENABLED=0 binary)

  # Release artifacts are per-OS/arch tarballs named by the release workflow:
  #   glove-v<version>-<goos>-<goarch>.tar.gz  (contents: <same-name>/ dir
  #   holding the binary of the same name + LICENSE + README.md)
  # The sha256 lines are filled per release from checksums.txt by
  # scripts/update-homebrew-sha.sh — a placeholder is a refused install,
  # which is the honest failure mode (verify-before-install everywhere).
  if OS.mac? && Hardware::CPU.arm?
    url "https://github.com/teochenglim/glove/releases/download/v#{version}/glove-v#{version}-darwin-arm64.tar.gz"
    sha256 "0000000000000000000000000000000000000000000000000000000000000000"

    def install
      bin.install "glove-v#{version}-darwin-arm64" => "glove"
    end
  elsif OS.mac? && Hardware::CPU.intel?
    url "https://github.com/teochenglim/glove/releases/download/v#{version}/glove-v#{version}-darwin-amd64.tar.gz"
    sha256 "0000000000000000000000000000000000000000000000000000000000000000"

    def install
      bin.install "glove-v#{version}-darwin-amd64" => "glove"
    end
  elsif OS.linux? && Hardware::CPU.arm? && Hardware::CPU.is_64_bit?
    url "https://github.com/teochenglim/glove/releases/download/v#{version}/glove-v#{version}-linux-arm64.tar.gz"
    sha256 "0000000000000000000000000000000000000000000000000000000000000000"

    def install
      bin.install "glove-v#{version}-linux-arm64" => "glove"
    end
  elsif OS.linux? && Hardware::CPU.intel?
    url "https://github.com/teochenglim/glove/releases/download/v#{version}/glove-v#{version}-linux-amd64.tar.gz"
    sha256 "0000000000000000000000000000000000000000000000000000000000000000"

    def install
      bin.install "glove-v#{version}-linux-amd64" => "glove"
    end
  end

  def caveats
    <<~EOS
      Install the hook into your agents with:
        glove install claude-code    # or kilo, codex, opencode, cursor, gemini
      Zed is audit-only (no pre-tool hook surface exists).
    EOS
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/glove version")
  end
end
