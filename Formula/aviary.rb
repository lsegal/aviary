class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.82"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.82/aviary_v0.7.82_darwin_arm64.tar.gz"
      sha256 "4a4321bb8574ac54165ed21c696232e2e950867445ae55630c7d20d7578b8b7a"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.82/aviary_v0.7.82_darwin_amd64.tar.gz"
      sha256 "42bc8e58cfdf56f8950ceefee085a4393431f170ae94128895c47d295b86034e"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.82/aviary_v0.7.82_linux_arm64.tar.gz"
      sha256 "0e444c320700812828954781ade91f9922babecf0d58d29838f63cd3a1e9a926"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.82/aviary_v0.7.82_linux_amd64.tar.gz"
      sha256 "437b42c1b695e5365cf641540a45c47adb336b893412f2f0c837e1550189d598"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
