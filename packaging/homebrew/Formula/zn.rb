class Zn < Formula
  desc "ZenNotes command-line interface and terminal app for Markdown notes"
  homepage "https://github.com/ZenNotes/tui"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/ZenNotes/tui/releases/download/v0.1.0/zn_0.1.0_darwin_arm64.tar.gz"
      sha256 "0ca4246b74d6a33f4228ed141fae843d394543864798ea383e0aba469593cfac"
    end
    on_intel do
      url "https://github.com/ZenNotes/tui/releases/download/v0.1.0/zn_0.1.0_darwin_amd64.tar.gz"
      sha256 "c2627b9ccd475137680e6d54635a2d740b31b40e7fd650bc7e4dd88d070da9ee"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/ZenNotes/tui/releases/download/v0.1.0/zn_0.1.0_linux_arm64.tar.gz"
      sha256 "644bdffe770632ec0debfe9b06561ca8ab014ff94fa802766b108c099ae3ee0d"
    end
    on_intel do
      url "https://github.com/ZenNotes/tui/releases/download/v0.1.0/zn_0.1.0_linux_amd64.tar.gz"
      sha256 "9211cdd1f243de4c6b044e55b7e20e17a9174dd38ae73410565b86022482188e"
    end
  end

  def install
    bin.install "zn"
  end

  test do
    ENV["ZENNOTES_CONFIG_DIR"] = (testpath/"config").to_s
    assert_match version.to_s, shell_output("#{bin}/zn --version")

    system bin/"zn", "init", testpath/"notes"
    (testpath/"notes/Homebrew.md").write("# Homebrew\n\nA note from the formula test.\n")
    assert_equal "# Homebrew\n\nA note from the formula test.\n",
                 shell_output("#{bin}/zn read Homebrew.md --vault #{testpath}/notes")
  end
end
