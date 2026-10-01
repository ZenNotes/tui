class Zn < Formula
  desc "ZenNotes command-line interface and terminal app for Markdown notes"
  homepage "https://github.com/ZenNotes/tui"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/ZenNotes/tui/releases/download/v0.6.2/zn_0.6.2_darwin_arm64.tar.gz"
      sha256 "1048e148ceba340ba4c5119fc8446566f4c522dacfe0855fe769b01820995d69"
    end
    on_intel do
      url "https://github.com/ZenNotes/tui/releases/download/v0.6.2/zn_0.6.2_darwin_amd64.tar.gz"
      sha256 "0aa804da788e58d4b75267e93100ef9beec9b787bc3d07c9d0bb94f00bdcbd22"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/ZenNotes/tui/releases/download/v0.6.2/zn_0.6.2_linux_arm64.tar.gz"
      sha256 "315f28c54084c3cfb8b7079c90a715584bef5baed5d4376e788673c15d0a4d06"
    end
    on_intel do
      url "https://github.com/ZenNotes/tui/releases/download/v0.6.2/zn_0.6.2_linux_amd64.tar.gz"
      sha256 "7ce88ea2d0081159a3c971d3dfa5f708decf58ed734b225139be5bb5f46856dd"
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
