class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.52"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.52/aviary_v0.7.52_darwin_arm64.tar.gz"
      sha256 "a202c39d470166a24b797018258056ff46f71e6b06a5d86f7e0e7b569e56a4bb"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.52/aviary_v0.7.52_darwin_amd64.tar.gz"
      sha256 "fb0d863d0861420075b20055e10285e1d58b701f4d52f01747c0e715208bf776"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.52/aviary_v0.7.52_linux_arm64.tar.gz"
      sha256 "98582ae6b974100592298ac2ef608959ac4ae3ccc88dea36be760c3797590b1b"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.52/aviary_v0.7.52_linux_amd64.tar.gz"
      sha256 "0a948a7337203301b9120ada37189377e72143a9bb3d06881182c2594770aceb"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
