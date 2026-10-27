class Zn < Formula
  desc "ZenNotes command-line interface and terminal app for Markdown notes"
  homepage "https://github.com/ZenNotes/tui"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/ZenNotes/tui/releases/download/v0.11.0/zn_0.11.0_darwin_arm64.tar.gz"
      sha256 "4b3548577ece83dc311bb67d72d4389a0a48a24c3bd56b9ba28aaa705303d33f"
    end
    on_intel do
      url "https://github.com/ZenNotes/tui/releases/download/v0.11.0/zn_0.11.0_darwin_amd64.tar.gz"
      sha256 "9dd6b9e19c3448ff947c7dd20eec511a6ffb4975616f0a78014c0e6b33047ddb"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/ZenNotes/tui/releases/download/v0.11.0/zn_0.11.0_linux_arm64.tar.gz"
      sha256 "132eb7755693cea9911783c2097b6a18964e18658a95d99d93353bc1e56ee6e8"
    end
    on_intel do
      url "https://github.com/ZenNotes/tui/releases/download/v0.11.0/zn_0.11.0_linux_amd64.tar.gz"
      sha256 "44fd68b60dd82946a210dc715bea4d8d28363870ef7fcc335522e0a3e586d2bd"
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
