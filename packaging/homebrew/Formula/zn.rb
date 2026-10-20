class Zn < Formula
  desc "ZenNotes command-line interface and terminal app for Markdown notes"
  homepage "https://github.com/ZenNotes/tui"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/ZenNotes/tui/releases/download/v0.9.1/zn_0.9.1_darwin_arm64.tar.gz"
      sha256 "6ee4b0d9ed60417453bae5aed10c1cb92cd2d1504be79afb5e6b8fb31caed99a"
    end
    on_intel do
      url "https://github.com/ZenNotes/tui/releases/download/v0.9.1/zn_0.9.1_darwin_amd64.tar.gz"
      sha256 "58c990a91900df44684f942d11907fec79c2712ca1a513278f957328779478c6"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/ZenNotes/tui/releases/download/v0.9.1/zn_0.9.1_linux_arm64.tar.gz"
      sha256 "b8a3dd4a3f160c2d5d9184a0a4ca941822d410623ef3c609aa841638dc866a54"
    end
    on_intel do
      url "https://github.com/ZenNotes/tui/releases/download/v0.9.1/zn_0.9.1_linux_amd64.tar.gz"
      sha256 "8064c1282f63ce520ba252c09be4d867c8b8b4417d88c547cd5ea4e74a2a84da"
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
