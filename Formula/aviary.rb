class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.18"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.18/aviary_v0.7.18_darwin_arm64.tar.gz"
      sha256 "3cd991b24bc2e3ede54be285cb665f43f5a50bddc314005245c7563a53e71aae"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.18/aviary_v0.7.18_darwin_amd64.tar.gz"
      sha256 "b5b62028815ba27dbb6a7819d466084665f969335f1bf8191ecd1f69aa4bbd91"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.18/aviary_v0.7.18_linux_arm64.tar.gz"
      sha256 "ac5557164a382f99e36500e2c03735885173770630d32254a6dfc87988d74bda"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.18/aviary_v0.7.18_linux_amd64.tar.gz"
      sha256 "a2447d728c329533832c9bae0f529c83cd971f74558a4fedfb953e8597022c47"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
