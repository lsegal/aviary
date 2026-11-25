class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.55"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.55/aviary_v0.7.55_darwin_arm64.tar.gz"
      sha256 "9dc084134d0b8fe8e461556bec3864d58a618b22444c36643712687a25f03b02"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.55/aviary_v0.7.55_darwin_amd64.tar.gz"
      sha256 "b6cc069800916c8cb58e3916b9d8f819895fc28fa544a6932a59c47a48254db9"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.55/aviary_v0.7.55_linux_arm64.tar.gz"
      sha256 "72ed2212e24e8e8eec95b6d399364bf8822b0792d4f5daa502cb9d24fbdcd41e"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.55/aviary_v0.7.55_linux_amd64.tar.gz"
      sha256 "16f3b4eb946cbe3d5c995c17d1bd7bee9fa988fe1186eb11b24067a095cd77fc"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
