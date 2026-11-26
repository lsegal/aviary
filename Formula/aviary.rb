class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.72"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.72/aviary_v0.7.72_darwin_arm64.tar.gz"
      sha256 "1eb412a6a5cdb12d74115f9d18b5fb4d9650cfa7d709ed13cc4bfdafb9121047"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.72/aviary_v0.7.72_darwin_amd64.tar.gz"
      sha256 "e406e179aa933a0203cdc5a88dcd5e71c801e96e71966ae3cbcc50965a66ca64"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.72/aviary_v0.7.72_linux_arm64.tar.gz"
      sha256 "63e708295b9651cd5f7e0ac533b51118c618d9022fab392b6ad8f46f46c9f9a2"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.72/aviary_v0.7.72_linux_amd64.tar.gz"
      sha256 "12fc9bfd16176d297e86c78276bce92407497238e0f28b8da317b72171a2abc7"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
