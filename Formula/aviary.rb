class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.40"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.40/aviary_v0.7.40_darwin_arm64.tar.gz"
      sha256 "b3937565107ff7bc6dd0e294b9b893e812c19c7aa226e03526ebd325a261f83b"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.40/aviary_v0.7.40_darwin_amd64.tar.gz"
      sha256 "8b6ba78fe350daf25b3dce3f31153735d4fe22750734cc214208318f44921dec"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.40/aviary_v0.7.40_linux_arm64.tar.gz"
      sha256 "eaabe84b5489325dad5bc0eb98dad13aa9cf2080be6ef415deaf2715fe4371be"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.40/aviary_v0.7.40_linux_amd64.tar.gz"
      sha256 "8edfc71520e9788117dae7e791869484dbdf69fc3f01510a1ddbbaa4a6a8ceb7"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
