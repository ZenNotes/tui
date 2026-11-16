class Zn < Formula
  desc "ZenNotes command-line interface and terminal app for Markdown notes"
  homepage "https://github.com/ZenNotes/tui"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/ZenNotes/tui/releases/download/v0.17.2/zn_0.17.2_darwin_arm64.tar.gz"
      sha256 "9d7e93470156642a87803f3301ac3d67f17053d07cd75f741e60c5c1230e1e41"
    end
    on_intel do
      url "https://github.com/ZenNotes/tui/releases/download/v0.17.2/zn_0.17.2_darwin_amd64.tar.gz"
      sha256 "6090d775a84153dd36082584cf5c95d0c724553537d718c8034d1f95041c2b17"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/ZenNotes/tui/releases/download/v0.17.2/zn_0.17.2_linux_arm64.tar.gz"
      sha256 "1294b0ebcc0ff4864ba8eb34aafbc995aa9b097056e19e71ad5ff429bea66409"
    end
    on_intel do
      url "https://github.com/ZenNotes/tui/releases/download/v0.17.2/zn_0.17.2_linux_amd64.tar.gz"
      sha256 "5f7d53f1563c0d9ae0f0d552e76e6cc63f0707f43e22f98e4edbe88637583b36"
    end
  end

  def install
    bin.install "zn"
  end

  def caveats
    <<~EOS
      zn renders display math with typst by default. To draw LaTeX math instead
      (math = "latex" under [terminal] in config.toml), zn runs pdftex and
      pdftocairo installed on your computer; it does not include TeX. On macOS:
        brew install --cask basictex
        brew install poppler
        sudo tlmgr install standalone varwidth preview xcolor amsmath amsfonts mhchem pgf
      On Linux, install your distribution's TeX Live and poppler packages.
      Run zn doctor to check; it prints the commands for your system.
    EOS
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
